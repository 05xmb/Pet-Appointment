package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/reporting"
)

const sessionCookie = "__Host-pet_session"

type Clock interface {
	ClockNow(context.Context, time.Time) (time.Time, error)
	AdvanceClock(context.Context, int64) error
}

type HandlerOption func(*handlerDependencies)

type handlerDependencies struct {
	bookingService      *booking.Service
	paymentService      *payment.Service
	cancellationService *cancellation.Service
	fulfillmentService  *fulfillment.Service
	reportingService    *reporting.Service
}

func WithBookingService(service *booking.Service) HandlerOption {
	return func(deps *handlerDependencies) { deps.bookingService = service }
}

func WithPaymentService(service *payment.Service) HandlerOption {
	return func(deps *handlerDependencies) { deps.paymentService = service }
}

func WithCancellationService(service *cancellation.Service) HandlerOption {
	return func(deps *handlerDependencies) { deps.cancellationService = service }
}

func WithFulfillmentService(service *fulfillment.Service) HandlerOption {
	return func(deps *handlerDependencies) { deps.fulfillmentService = service }
}

func WithReportingService(service *reporting.Service) HandlerOption {
	return func(deps *handlerDependencies) { deps.reportingService = service }
}

func NewHandler(service *auth.Service, clock Clock, options ...HandlerOption) http.Handler {
	deps := &handlerDependencies{}
	for _, option := range options {
		option(deps)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	loginLimit := newLoginLimiter(time.Now)
	mux.HandleFunc("GET /", serveDashboard)
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		username := r.PostForm.Get("username")
		if !loginLimit.allow(r.RemoteAddr, username) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many login attempts", http.StatusTooManyRequests)
			return
		}
		token, err := service.Login(r.Context(), username, r.PostForm.Get("password"))
		if errors.Is(err, auth.ErrUnauthorized) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		loginLimit.succeeded(r.RemoteAddr, username)
		if err != nil {
			http.Error(w, "login unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: 8 * 3600, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, service)
		if !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(identity)
	})
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := service.Logout(r.Context(), cookie.Value); err != nil {
			http.Error(w, "logout unavailable", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /clock", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		if _, ok := sessionIdentity(w, r, service); !ok {
			return
		}
		current, err := clock.ClockNow(r.Context(), time.Now())
		if err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"now": current.In(shanghai).Format(time.RFC3339)})
	})
	mux.HandleFunc("POST /clock/advance", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, service)
		if !ok {
			return
		}
		if identity.Role != "manager" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		minutes, err := strconv.ParseInt(r.PostForm.Get("minutes"), 10, 64)
		if err != nil || minutes <= 0 || minutes > 365*24*60 {
			http.Error(w, "invalid clock advance", http.StatusBadRequest)
			return
		}
		if err := clock.AdvanceClock(r.Context(), minutes); err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if deps.bookingService != nil {
		registerBookingHandlers(mux, service, clock, deps.bookingService)
	}
	if deps.paymentService != nil {
		registerPaymentHandlers(mux, service, clock, deps.paymentService)
	}
	if deps.cancellationService != nil {
		registerCancellationHandlers(mux, service, clock, deps.cancellationService)
	}
	if deps.fulfillmentService != nil {
		registerFulfillmentHandlers(mux, service, clock, deps.fulfillmentService)
	}
	if deps.reportingService != nil {
		registerReportingHandlers(mux, service, deps.reportingService, shanghai)
	}
	return http.NewCrossOriginProtection().Handler(mux)
}

func registerReportingHandlers(mux *http.ServeMux, authService *auth.Service, reportingService *reporting.Service, loc *time.Location) {
	mux.HandleFunc("GET /bookings", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		from, to, date, err := reportDayRange(r.URL.Query().Get("date"), loc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		views, err := reportingService.ListBookings(r.Context(), identity, from, to)
		if err != nil {
			writeReportingError(w, err)
			return
		}
		writeReporting(w, http.StatusOK, map[string]any{"date": date, "bookings": views})
	})

	mux.HandleFunc("GET /daily-summary", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		from, to, date, err := reportDayRange(r.URL.Query().Get("date"), loc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		summary, err := reportingService.DailySummary(r.Context(), identity, from, to, date)
		if err != nil {
			writeReportingError(w, err)
			return
		}
		writeReporting(w, http.StatusOK, summary)
	})
}

func reportDayRange(value string, loc *time.Location) (time.Time, time.Time, string, error) {
	if value == "" {
		return time.Time{}, time.Time{}, "", reporting.ErrInvalidDate
	}
	day, err := time.ParseInLocation("2006-01-02", value, loc)
	if err != nil {
		return time.Time{}, time.Time{}, "", reporting.ErrInvalidDate
	}
	return day.UTC(), day.AddDate(0, 0, 1).UTC(), value, nil
}

func registerCancellationHandlers(mux *http.ServeMux, authService *auth.Service, clock Clock, cancellationService *cancellation.Service) {
	mux.HandleFunc("POST /bookings/{id}/cancel-request", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		if identity.Role != "owner" || identity.OwnerID == 0 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id, err := parsePathID(r, "booking")
		if err != nil {
			http.Error(w, "invalid booking", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil || strings.TrimSpace(r.PostForm.Get("reason")) == "" {
			http.Error(w, "reason is required", http.StatusBadRequest)
			return
		}
		now, err := clock.ClockNow(r.Context(), time.Now())
		if err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		result, err := cancellationService.Request(r.Context(), identity.OwnerID, id, r.PostForm.Get("reason"), now)
		if err != nil {
			writeCancellationError(w, err)
			return
		}
		writeCancellation(w, http.StatusCreated, result)
	})

	mux.HandleFunc("POST /cancellation-requests/{id}/review", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		if identity.Role != "support" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id, err := parsePathID(r, "cancellation request")
		if err != nil {
			http.Error(w, "invalid cancellation request", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		approve, err := strconv.ParseBool(r.PostForm.Get("approve"))
		if err != nil {
			http.Error(w, "approve must be true or false", http.StatusBadRequest)
			return
		}
		now, err := clock.ClockNow(r.Context(), time.Now())
		if err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		result, err := cancellationService.Review(r.Context(), identity.ID, id, approve, now)
		if err != nil {
			writeCancellationError(w, err)
			return
		}
		writeBooking(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /refunds/callback", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid callback", http.StatusBadRequest)
			return
		}
		result, err := cancellationService.RefundCallback(r.Context(), raw, r.Header.Get("X-Signature"))
		if err != nil {
			writeCancellationError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
}

func registerFulfillmentHandlers(mux *http.ServeMux, authService *auth.Service, clock Clock, fulfillmentService *fulfillment.Service) {
	registerFulfillmentAction := func(pattern, role string, action func(context.Context, auth.Identity, int64, time.Time) (booking.Booking, fulfillment.Event, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !requireTLS(w, r) {
				return
			}
			identity, ok := sessionIdentity(w, r, authService)
			if !ok {
				return
			}
			if identity.Role != role {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			id, err := parsePathID(r, "booking")
			if err != nil {
				http.Error(w, "invalid booking", http.StatusBadRequest)
				return
			}
			now, err := clock.ClockNow(r.Context(), time.Now())
			if err != nil {
				http.Error(w, "clock unavailable", http.StatusInternalServerError)
				return
			}
			result, event, err := action(r.Context(), identity, id, now)
			if err != nil {
				writeFulfillmentError(w, err)
				return
			}
			writeFulfillment(w, http.StatusOK, result, event)
		})
	}
	registerFulfillmentAction("POST /bookings/{id}/check-in", "desk", func(ctx context.Context, identity auth.Identity, id int64, now time.Time) (booking.Booking, fulfillment.Event, error) {
		return fulfillmentService.CheckIn(ctx, identity.ID, id, now)
	})
	registerFulfillmentAction("POST /bookings/{id}/service/start", "worker", func(ctx context.Context, identity auth.Identity, id int64, now time.Time) (booking.Booking, fulfillment.Event, error) {
		return fulfillmentService.Start(ctx, identity.ID, id, now)
	})
	registerFulfillmentAction("POST /bookings/{id}/service/complete", "worker", func(ctx context.Context, identity auth.Identity, id int64, now time.Time) (booking.Booking, fulfillment.Event, error) {
		return fulfillmentService.Complete(ctx, identity.ID, id, now)
	})
	mux.HandleFunc("POST /bookings/{id}/no-show", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		if identity.Role != "manager" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id, err := parsePathID(r, "booking")
		if err != nil {
			http.Error(w, "invalid booking", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil || strings.TrimSpace(r.PostForm.Get("reason")) == "" {
			http.Error(w, "reason is required", http.StatusBadRequest)
			return
		}
		now, err := clock.ClockNow(r.Context(), time.Now())
		if err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		result, event, err := fulfillmentService.NoShow(r.Context(), identity.ID, id, r.PostForm.Get("reason"), now)
		if err != nil {
			writeFulfillmentError(w, err)
			return
		}
		writeFulfillment(w, http.StatusOK, result, event)
	})
}

func registerPaymentHandlers(mux *http.ServeMux, authService *auth.Service, clock Clock, paymentService *payment.Service) {
	mux.HandleFunc("POST /payments", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		if identity.Role != "owner" || identity.OwnerID == 0 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		bookingID, err := strconv.ParseInt(r.PostForm.Get("booking_id"), 10, 64)
		if err != nil || bookingID <= 0 {
			http.Error(w, "invalid booking", http.StatusBadRequest)
			return
		}
		now, err := clock.ClockNow(r.Context(), time.Now())
		if err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		result, err := paymentService.CreatePayment(r.Context(), identity.OwnerID, bookingID, now)
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writePayment(w, http.StatusCreated, result)
	})
	mux.HandleFunc("POST /payments/callback", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid callback", http.StatusBadRequest)
			return
		}
		now, err := clock.ClockNow(r.Context(), time.Now())
		if err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		result, err := paymentService.Callback(r.Context(), raw, r.Header.Get("X-Signature"), now)
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writePayment(w, http.StatusOK, result)
	})
}

func writePayment(w http.ResponseWriter, status int, result payment.Payment) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

func writePaymentError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, payment.ErrInvalidSignature):
		status = http.StatusUnauthorized
	case errors.Is(err, payment.ErrInvalidCallback):
		status = http.StatusBadRequest
	case errors.Is(err, payment.ErrPaymentNotFound):
		status = http.StatusNotFound
	case errors.Is(err, payment.ErrPaymentAmountMismatch), errors.Is(err, payment.ErrBookingNotPayable):
		status = http.StatusConflict
	case errors.Is(err, booking.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, booking.ErrNotFound):
		status = http.StatusNotFound
	}
	http.Error(w, err.Error(), status)
}

func writeCancellation(w http.ResponseWriter, status int, result cancellation.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

func writeCancellationError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, cancellation.ErrInvalidSignature):
		status = http.StatusUnauthorized
	case errors.Is(err, cancellation.ErrInvalidCallback):
		status = http.StatusBadRequest
	case errors.Is(err, cancellation.ErrForbidden), errors.Is(err, booking.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, cancellation.ErrInvalidState), errors.Is(err, cancellation.ErrTooEarly), errors.Is(err, cancellation.ErrTooLate):
		status = http.StatusConflict
	case errors.Is(err, booking.ErrNotFound):
		status = http.StatusNotFound
	}
	http.Error(w, err.Error(), status)
}

func writeFulfillment(w http.ResponseWriter, status int, result booking.Booking, event fulfillment.Event) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"booking": result, "event": event})
}

func writeFulfillmentError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, fulfillment.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, fulfillment.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, fulfillment.ErrInvalidState), errors.Is(err, fulfillment.ErrOutsideCheckInWindow), errors.Is(err, fulfillment.ErrTooEarly):
		status = http.StatusConflict
	case errors.Is(err, fulfillment.ErrReasonRequired):
		status = http.StatusBadRequest
	}
	http.Error(w, err.Error(), status)
}

func writeReporting(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeReportingError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, reporting.ErrForbidden) {
		status = http.StatusForbidden
	}
	http.Error(w, err.Error(), status)
}

func parsePathID(r *http.Request, _ string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func registerBookingHandlers(mux *http.ServeMux, authService *auth.Service, clock Clock, bookingService *booking.Service) {
	mux.HandleFunc("POST /bookings", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		if identity.Role != "owner" || identity.OwnerID == 0 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		petID, slotID, err := parseIDs(r.PostForm.Get("pet_id"), r.PostForm.Get("slot_id"))
		if err != nil {
			http.Error(w, "invalid pet or slot", http.StatusBadRequest)
			return
		}
		now, err := clock.ClockNow(r.Context(), time.Now())
		if err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		result, err := bookingService.Book(r.Context(), identity.OwnerID, petID, slotID, now)
		if err != nil {
			writeBookingError(w, err)
			return
		}
		writeBooking(w, http.StatusCreated, result)
	})
	mux.HandleFunc("POST /bookings/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		if identity.Role != "owner" || identity.OwnerID == 0 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid booking", http.StatusBadRequest)
			return
		}
		result, err := bookingService.CancelUnpaid(r.Context(), identity.OwnerID, id)
		if err != nil {
			writeBookingError(w, err)
			return
		}
		writeBooking(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /bookings/{id}/expire", func(w http.ResponseWriter, r *http.Request) {
		if !requireTLS(w, r) {
			return
		}
		identity, ok := sessionIdentity(w, r, authService)
		if !ok {
			return
		}
		if identity.Role != "manager" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid booking", http.StatusBadRequest)
			return
		}
		now, err := clock.ClockNow(r.Context(), time.Now())
		if err != nil {
			http.Error(w, "clock unavailable", http.StatusInternalServerError)
			return
		}
		result, err := bookingService.CloseExpired(r.Context(), id, now)
		if err != nil {
			writeBookingError(w, err)
			return
		}
		writeBooking(w, http.StatusOK, result)
	})
}

func parseIDs(petValue, slotValue string) (int64, int64, error) {
	petID, err := strconv.ParseInt(petValue, 10, 64)
	if err != nil || petID <= 0 {
		return 0, 0, errors.New("invalid pet")
	}
	slotID, err := strconv.ParseInt(slotValue, 10, 64)
	if err != nil || slotID <= 0 {
		return 0, 0, errors.New("invalid slot")
	}
	return petID, slotID, nil
}

func writeBooking(w http.ResponseWriter, status int, result booking.Booking) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(result)
}

func writeBookingError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, booking.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, booking.ErrNotFound), errors.Is(err, booking.ErrPetNotFound):
		status = http.StatusNotFound
	case errors.Is(err, booking.ErrDuplicatePet), errors.Is(err, booking.ErrCapacityFull), errors.Is(err, booking.ErrSlotStarted), errors.Is(err, booking.ErrInvalidState), errors.Is(err, booking.ErrNotExpired):
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
}

func requireTLS(w http.ResponseWriter, r *http.Request) bool {
	if r.TLS == nil {
		http.Error(w, "HTTPS required", http.StatusForbidden)
		return false
	}
	return true
}

func sessionIdentity(w http.ResponseWriter, r *http.Request, service *auth.Service) (auth.Identity, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return auth.Identity{}, false
	}
	identity, err := service.Authenticate(r.Context(), cookie.Value)
	if errors.Is(err, auth.ErrUnauthorized) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return auth.Identity{}, false
	}
	if err != nil {
		http.Error(w, "session unavailable", http.StatusInternalServerError)
		return auth.Identity{}, false
	}
	return identity, true
}
