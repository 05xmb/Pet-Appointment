package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/05xmb/Pet-Appointment/internal/booking"
)

type bookingRepository struct {
	created   booking.Booking
	createErr error
	cancelled booking.Booking
	cancelErr error
	closed    booking.Booking
	closeErr  error
	ownerID   int64
}

func (r *bookingRepository) CreatePending(_ context.Context, ownerID, _, _ int64, _ time.Time) (booking.Booking, error) {
	r.ownerID = ownerID
	if r.createErr != nil {
		return booking.Booking{}, r.createErr
	}
	return r.created, nil
}

func (r *bookingRepository) CancelPending(_ context.Context, ownerID, _ int64) (booking.Booking, error) {
	r.ownerID = ownerID
	if r.cancelErr != nil {
		return booking.Booking{}, r.cancelErr
	}
	return r.cancelled, nil
}

func (r *bookingRepository) CloseExpired(_ context.Context, _ int64, _ time.Time) (booking.Booking, error) {
	if r.closeErr != nil {
		return booking.Booking{}, r.closeErr
	}
	return r.closed, nil
}

func bookingHandler(t *testing.T, role string, repo *bookingRepository) (http.Handler, http.Cookie) {
	t.Helper()
	h, clock, accountRepo := testFixtureForRole(t, role)
	_ = h
	h = NewHandler(accountService(accountRepo), clock, WithBookingService(booking.NewService(repo)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loginRequest("https"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("login status=%d body=%q", w.Code, w.Body.String())
	}
	return h, *w.Result().Cookies()[0]
}

func accountService(repo *repository) *auth.Service {
	return auth.NewService(repo, time.Now)
}

func TestBookingHTTPUsesSessionOwnerAndRejectsForgedUserID(t *testing.T) {
	repo := &bookingRepository{created: booking.Booking{ID: 41, OwnerID: 7, PetID: 1, SlotID: 1, Status: booking.StatusPendingPayment}}
	h, cookie := bookingHandler(t, "owner", repo)
	form := url.Values{"pet_id": {"1"}, "slot_id": {"1"}, "user_id": {"999"}}
	req := httptest.NewRequest(http.MethodPost, "https://example.test/bookings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || repo.ownerID != 7 {
		t.Fatalf("status=%d owner=%d body=%q", w.Code, repo.ownerID, w.Body.String())
	}
	var got booking.Booking
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil || got.ID != 41 {
		t.Fatalf("response=%+v error=%v", got, err)
	}
}

func TestBookingHTTPMapsConflictsAndRequiresOwnerSession(t *testing.T) {
	repo := &bookingRepository{createErr: booking.ErrCapacityFull}
	h, cookie := bookingHandler(t, "owner", repo)
	form := url.Values{"pet_id": {"1"}, "slot_id": {"1"}}
	req := httptest.NewRequest(http.MethodPost, "https://example.test/bookings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("capacity status=%d body=%q", w.Code, w.Body.String())
	}
	noSession := httptest.NewRequest(http.MethodPost, "https://example.test/bookings", strings.NewReader(form.Encode()))
	noSession.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, noSession)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status=%d", w.Code)
	}
	manager, managerCookie := bookingHandler(t, "manager", &bookingRepository{createErr: errors.New("should not be called")})
	managerReq := httptest.NewRequest(http.MethodPost, "https://example.test/bookings", strings.NewReader(form.Encode()))
	managerReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	managerReq.AddCookie(&managerCookie)
	w = httptest.NewRecorder()
	manager.ServeHTTP(w, managerReq)
	if w.Code != http.StatusForbidden {
		t.Fatalf("manager booking status=%d", w.Code)
	}
}

func TestCancelAndExpireHTTPUseProtectedActions(t *testing.T) {
	repo := &bookingRepository{
		cancelled: booking.Booking{ID: 41, OwnerID: 7, Status: booking.StatusCancelled},
		closed:    booking.Booking{ID: 42, Status: booking.StatusClosed},
	}
	h, cookie := bookingHandler(t, "owner", repo)
	cancel := httptest.NewRequest(http.MethodPost, "https://example.test/bookings/41/cancel", nil)
	cancel.AddCookie(&cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, cancel)
	if w.Code != http.StatusOK || repo.ownerID != 7 {
		t.Fatalf("cancel status=%d owner=%d body=%q", w.Code, repo.ownerID, w.Body.String())
	}
	manager, managerCookie := bookingHandler(t, "manager", repo)
	expire := httptest.NewRequest(http.MethodPost, "https://example.test/bookings/42/expire", nil)
	expire.AddCookie(&managerCookie)
	w = httptest.NewRecorder()
	manager.ServeHTTP(w, expire)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "CLOSED") {
		t.Fatalf("expire status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestBookingActionsRejectInvalidPathIDs(t *testing.T) {
	repo := &bookingRepository{}
	h, cookie := bookingHandler(t, "owner", repo)
	cancel := httptest.NewRequest(http.MethodPost, "https://example.test/bookings/bad/cancel", nil)
	cancel.AddCookie(&cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, cancel)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid cancel id status=%d body=%q", w.Code, w.Body.String())
	}
	manager, managerCookie := bookingHandler(t, "manager", repo)
	expire := httptest.NewRequest(http.MethodPost, "https://example.test/bookings/bad/expire", nil)
	expire.AddCookie(&managerCookie)
	w = httptest.NewRecorder()
	manager.ServeHTTP(w, expire)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid expire id status=%d body=%q", w.Code, w.Body.String())
	}
}
