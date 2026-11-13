package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/payment"
)

type paymentRepository struct {
	created       payment.Payment
	createErr     error
	callback      payment.Payment
	callbackErr   error
	callbackCalls int
}

func (r *paymentRepository) CreatePayment(_ context.Context, _ int64, _ int64, _ time.Time) (payment.Payment, error) {
	if r.createErr != nil {
		return payment.Payment{}, r.createErr
	}
	return r.created, nil
}

func (r *paymentRepository) ApplyCallback(_ context.Context, _ payment.Callback, _ time.Time) (payment.Payment, error) {
	r.callbackCalls++
	if r.callbackErr != nil {
		return payment.Payment{}, r.callbackErr
	}
	return r.callback, nil
}

func TestPaymentHTTPUsesSessionOwnerAndDoesNotAcceptRequestAmount(t *testing.T) {
	repo := &paymentRepository{created: payment.Payment{ID: 5, BookingID: 41, OwnerID: 7, AmountCents: 9900, Status: payment.StatusPending}}
	h, clock, accountRepo := testFixtureForRole(t, "owner")
	_ = h
	paymentService := payment.NewService(repo, []byte("test-secret"))
	h = NewHandler(accountService(accountRepo), clock, WithPaymentService(paymentService))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loginRequest("https"))
	cookie := w.Result().Cookies()[0]
	form := url.Values{"booking_id": {"41"}, "amount_cents": {"1"}}
	req := httptest.NewRequest(http.MethodPost, "https://example.test/payments", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), "9900") {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestPaymentCallbackRequiresHMACAndIsIdempotentAtHTTPBoundary(t *testing.T) {
	repo := &paymentRepository{callback: payment.Payment{ID: 5, BookingID: 41, AmountCents: 9900, Status: payment.StatusSuccess}}
	_, clock, accountRepo := testFixtureForRole(t, "owner")
	h := NewHandler(accountService(accountRepo), clock, WithPaymentService(payment.NewService(repo, []byte("test-secret"))))
	raw := []byte(`{"payment_id":5,"booking_id":41,"outcome":"success","amount_cents":9900}`)
	call := func(signature string, body []byte) int {
		req := httptest.NewRequest(http.MethodPost, "https://example.test/payments/callback", strings.NewReader(string(body)))
		req.Header.Set("X-Signature", signature)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	if got := call("bad", raw); got != http.StatusUnauthorized || repo.callbackCalls != 0 {
		t.Fatalf("bad signature status=%d calls=%d", got, repo.callbackCalls)
	}
	if got := call(payment.Sign([]byte("test-secret"), raw), raw); got != http.StatusOK || repo.callbackCalls != 1 {
		t.Fatalf("valid callback status=%d calls=%d", got, repo.callbackCalls)
	}
	if got := call(payment.Sign([]byte("test-secret"), raw), raw); got != http.StatusOK || repo.callbackCalls != 2 {
		t.Fatalf("repeat callback status=%d calls=%d", got, repo.callbackCalls)
	}
	if got := call(payment.Sign([]byte("test-secret"), []byte("{}")), []byte("{}")); got != http.StatusBadRequest {
		t.Fatalf("invalid callback status=%d", got)
	}
}

func TestPaymentCallbackHTTPRejectsInvalidPayloadAndClockFailure(t *testing.T) {
	repo := &paymentRepository{callback: payment.Payment{ID: 5, BookingID: 41, AmountCents: 9900, Status: payment.StatusSuccess}}
	h, clock, accountRepo := testFixtureForRole(t, "owner")
	h = NewHandler(accountService(accountRepo), clock, WithPaymentService(payment.NewService(repo, []byte("test-secret"))))
	raw := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "https://example.test/payments/callback", strings.NewReader(string(raw)))
	req.Header.Set("X-Signature", payment.Sign([]byte("test-secret"), raw))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid payload status=%d body=%q", w.Code, w.Body.String())
	}
	clock.readErr = errors.New("clock failed")
	raw = []byte(`{"payment_id":5,"booking_id":41,"outcome":"success","amount_cents":9900}`)
	req = httptest.NewRequest(http.MethodPost, "https://example.test/payments/callback", strings.NewReader(string(raw)))
	req.Header.Set("X-Signature", payment.Sign([]byte("test-secret"), raw))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("clock failure status=%d body=%q", w.Code, w.Body.String())
	}
}
