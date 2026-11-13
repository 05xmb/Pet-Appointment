package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/refund"
)

type cancellationRepository struct {
	request    cancellation.Request
	requestErr error
	review     booking.Booking
	reviewErr  error
	refund     refund.Refund
	refundErr  error
	refundID   int64
	refundOK   bool
	ownerID    int64
	supportID  int64
}

func (r *cancellationRepository) CancelConfirmed(context.Context, int64, int64, time.Time) (booking.Booking, error) {
	return booking.Booking{}, nil
}

func (r *cancellationRepository) RequestCancellation(_ context.Context, ownerID, _ int64, _ string, _ time.Time) (cancellation.Request, error) {
	r.ownerID = ownerID
	return r.request, r.requestErr
}

func (r *cancellationRepository) ReviewCancellation(_ context.Context, supportID, _ int64, _ bool, _ time.Time) (booking.Booking, error) {
	r.supportID = supportID
	return r.review, r.reviewErr
}

func (r *cancellationRepository) ApplyRefundResult(_ context.Context, refundID int64, success bool) (refund.Refund, error) {
	r.refundID, r.refundOK = refundID, success
	return r.refund, r.refundErr
}

func TestCancellationHTTPUsesSessionIdentityAndMapsRoles(t *testing.T) {
	repo := &cancellationRepository{request: cancellation.Request{ID: 9, BookingID: 41, OwnerID: 7, Status: cancellation.StatusPending}}
	h, clock, accountRepo := testFixtureForRole(t, "owner")
	h = NewHandler(accountService(accountRepo), clock, WithCancellationService(cancellation.NewService(repo)))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, loginRequest("https"))
	cookie := login.Result().Cookies()[0]
	body := url.Values{"reason": {"临时有事"}, "owner_id": {"999"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "https://example.test/bookings/41/cancel-request", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || repo.ownerID != 7 || !strings.Contains(w.Body.String(), "PENDING") {
		t.Fatalf("request status=%d owner=%d body=%q", w.Code, repo.ownerID, w.Body.String())
	}

	repo.requestErr = cancellation.ErrInvalidState
	req = httptest.NewRequest(http.MethodPost, "https://example.test/bookings/41/cancel-request", strings.NewReader("reason=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("request conflict status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestCancellationReviewHTTPRequiresSupportSession(t *testing.T) {
	repo := &cancellationRepository{review: booking.Booking{ID: 41, Status: booking.StatusCancelled}}
	h, clock, accountRepo := testFixtureForRole(t, "support")
	h = NewHandler(accountService(accountRepo), clock, WithCancellationService(cancellation.NewService(repo)))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, loginRequest("https"))
	cookie := login.Result().Cookies()[0]
	req := httptest.NewRequest(http.MethodPost, "https://example.test/cancellation-requests/9/review", strings.NewReader("approve=true&support_id=999"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || repo.supportID != 7 || !strings.Contains(w.Body.String(), "CANCELLED") {
		t.Fatalf("review status=%d support=%d body=%q", w.Code, repo.supportID, w.Body.String())
	}
}
