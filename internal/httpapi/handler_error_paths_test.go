package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
	"github.com/05xmb/Pet-Appointment/internal/payment"
)

func requestWithCookie(h http.Handler, method, target string, cookie *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestBookingAndPaymentHTTPRejectBadInputsAndClockFailure(t *testing.T) {
	bookingRepo := &bookingRepository{createErr: booking.ErrCapacityFull}
	h, clock, accountRepo := testFixtureForRole(t, "owner")
	h = NewHandler(accountService(accountRepo), clock, WithBookingService(booking.NewService(bookingRepo)), WithPaymentService(payment.NewService(&paymentRepository{}, []byte("secret"))))
	cookie := loginCookie(t, h)
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/bookings", cookie, url.Values{"pet_id": {"bad"}, "slot_id": {"1"}}).Code; got != http.StatusBadRequest {
		t.Fatalf("bad booking ids status=%d", got)
	}
	clock.readErr = errors.New("clock failed")
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/bookings", cookie, url.Values{"pet_id": {"1"}, "slot_id": {"1"}}).Code; got != http.StatusInternalServerError {
		t.Fatalf("booking clock status=%d", got)
	}
	clock.readErr = nil
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/payments", cookie, url.Values{"booking_id": {"bad"}}).Code; got != http.StatusBadRequest {
		t.Fatalf("bad payment id status=%d", got)
	}
	clock.readErr = errors.New("clock failed")
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/payments", cookie, url.Values{"booking_id": {"41"}}).Code; got != http.StatusInternalServerError {
		t.Fatalf("payment clock status=%d", got)
	}
}

func TestCancellationAndFulfillmentHTTPRejectBadInputsAndClockFailure(t *testing.T) {
	cancelRepo := &cancellationRepository{}
	h, clock, accountRepo := testFixtureForRole(t, "owner")
	h = NewHandler(accountService(accountRepo), clock, WithCancellationService(cancellation.NewService(cancelRepo)))
	cookie := loginCookie(t, h)
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/bookings/bad/cancel-request", cookie, url.Values{"reason": {"x"}}).Code; got != http.StatusBadRequest {
		t.Fatalf("bad cancellation id status=%d", got)
	}
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/bookings/41/cancel-request", cookie, url.Values{}).Code; got != http.StatusBadRequest {
		t.Fatalf("missing cancellation reason status=%d", got)
	}
	clock.readErr = errors.New("clock failed")
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/bookings/41/cancel-request", cookie, url.Values{"reason": {"x"}}).Code; got != http.StatusInternalServerError {
		t.Fatalf("cancellation clock status=%d", got)
	}

	h, clock, accountRepo = testFixtureForRole(t, "support")
	h = NewHandler(accountService(accountRepo), clock, WithCancellationService(cancellation.NewService(cancelRepo)))
	cookie = loginCookie(t, h)
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/cancellation-requests/9/review", cookie, url.Values{"approve": {"maybe"}}).Code; got != http.StatusBadRequest {
		t.Fatalf("bad review approve status=%d", got)
	}
	clock.readErr = errors.New("clock failed")
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/cancellation-requests/9/review", cookie, url.Values{"approve": {"true"}}).Code; got != http.StatusInternalServerError {
		t.Fatalf("review clock status=%d", got)
	}

	h, clock, accountRepo = testFixtureForRole(t, "desk")
	h = NewHandler(accountService(accountRepo), clock, WithFulfillmentService(fulfillment.NewService(&fulfillmentRepository{})))
	cookie = loginCookie(t, h)
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/bookings/bad/check-in", cookie, nil).Code; got != http.StatusBadRequest {
		t.Fatalf("bad check-in id status=%d", got)
	}
	clock.readErr = errors.New("clock failed")
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/bookings/41/check-in", cookie, nil).Code; got != http.StatusInternalServerError {
		t.Fatalf("check-in clock status=%d", got)
	}

	h, clock, accountRepo = testFixtureForRole(t, "manager")
	h = NewHandler(accountService(accountRepo), clock, WithFulfillmentService(fulfillment.NewService(&fulfillmentRepository{})))
	cookie = loginCookie(t, h)
	if got := requestWithCookie(h, http.MethodPost, "https://example.test/bookings/41/no-show", cookie, url.Values{}).Code; got != http.StatusBadRequest {
		t.Fatalf("missing no-show reason status=%d", got)
	}
}

func TestLoginHTTPRateLimitsRepeatedFailures(t *testing.T) {
	h := testHandler(t)
	bad := url.Values{"username": {"owner"}, "password": {"wrong"}}
	for i := 0; i < 5; i++ {
		request := httptest.NewRequest(http.MethodPost, "https://example.test/login", strings.NewReader(bad.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.RemoteAddr = "127.0.0.1:9999"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d", i+1, w.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "https://example.test/login", strings.NewReader(bad.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "127.0.0.1:9999"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limit status=%d body=%q", w.Code, w.Body.String())
	}
}
