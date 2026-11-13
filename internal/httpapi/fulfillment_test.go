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
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
)

type fulfillmentRepository struct {
	booking   booking.Booking
	event     fulfillment.Event
	err       error
	actorID   int64
	managerID int64
	bookingID int64
	reason    string
}

func (r *fulfillmentRepository) CheckIn(_ context.Context, actorID, bookingID int64, _ time.Time) (booking.Booking, fulfillment.Event, error) {
	r.actorID, r.bookingID = actorID, bookingID
	return r.booking, r.event, r.err
}

func (r *fulfillmentRepository) StartService(_ context.Context, actorID, bookingID int64, _ time.Time) (booking.Booking, fulfillment.Event, error) {
	r.actorID, r.bookingID = actorID, bookingID
	return r.booking, r.event, r.err
}

func (r *fulfillmentRepository) CompleteService(_ context.Context, actorID, bookingID int64, _ time.Time) (booking.Booking, fulfillment.Event, error) {
	r.actorID, r.bookingID = actorID, bookingID
	return r.booking, r.event, r.err
}

func (r *fulfillmentRepository) MarkNoShow(_ context.Context, actorID, bookingID int64, reason string, _ time.Time) (booking.Booking, fulfillment.Event, error) {
	r.managerID, r.bookingID, r.reason = actorID, bookingID, reason
	return r.booking, r.event, r.err
}

func TestFulfillmentHTTPUsesSessionIdentityAndRoleBoundaries(t *testing.T) {
	repo := &fulfillmentRepository{booking: booking.Booking{ID: 41, Status: booking.StatusCheckedIn}}
	h, clock, accountRepo := testFixtureForRole(t, "desk")
	h = NewHandler(accountService(accountRepo), clock, WithFulfillmentService(fulfillment.NewService(repo)))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, loginRequest("https"))
	cookie := login.Result().Cookies()[0]
	req := httptest.NewRequest(http.MethodPost, "https://example.test/bookings/41/check-in?staff_id=999", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || repo.actorID != 7 || repo.bookingID != 41 {
		t.Fatalf("check-in status=%d actor=%d booking=%d body=%q", w.Code, repo.actorID, repo.bookingID, w.Body.String())
	}

	owner, ownerClock, ownerRepo := testFixtureForRole(t, "owner")
	owner = NewHandler(accountService(ownerRepo), ownerClock, WithFulfillmentService(fulfillment.NewService(&fulfillmentRepository{})))
	ownerLogin := httptest.NewRecorder()
	owner.ServeHTTP(ownerLogin, loginRequest("https"))
	ownerReq := httptest.NewRequest(http.MethodPost, "https://example.test/bookings/41/check-in", nil)
	ownerReq.AddCookie(ownerLogin.Result().Cookies()[0])
	w = httptest.NewRecorder()
	owner.ServeHTTP(w, ownerReq)
	if w.Code != http.StatusForbidden {
		t.Fatalf("owner check-in status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestNoShowHTTPRequiresReasonAndMapsConflict(t *testing.T) {
	repo := &fulfillmentRepository{booking: booking.Booking{ID: 41, Status: booking.StatusNoShow}, err: fulfillment.ErrInvalidState}
	h, clock, accountRepo := testFixtureForRole(t, "manager")
	h = NewHandler(accountService(accountRepo), clock, WithFulfillmentService(fulfillment.NewService(repo)))
	login := httptest.NewRecorder()
	h.ServeHTTP(login, loginRequest("https"))
	cookie := login.Result().Cookies()[0]
	body := url.Values{"reason": {"未到店"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "https://example.test/bookings/41/no-show", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusConflict || repo.managerID != 7 || repo.reason != "未到店" {
		t.Fatalf("no-show status=%d manager=%d reason=%q body=%q", w.Code, repo.managerID, repo.reason, w.Body.String())
	}
}

func TestServiceStartAndCompleteHTTPUseWorkerSession(t *testing.T) {
	repo := &fulfillmentRepository{booking: booking.Booking{ID: 41, Status: booking.StatusInService}}
	h, clock, accountRepo := testFixtureForRole(t, "worker")
	h = NewHandler(accountService(accountRepo), clock, WithFulfillmentService(fulfillment.NewService(repo)))
	cookie := loginCookie(t, h)
	for _, path := range []string{"/bookings/41/service/start", "/bookings/41/service/complete"} {
		req := httptest.NewRequest(http.MethodPost, "https://example.test"+path, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK || repo.actorID != 7 || repo.bookingID != 41 {
			t.Fatalf("path=%s status=%d actor=%d booking=%d body=%q", path, w.Code, repo.actorID, repo.bookingID, w.Body.String())
		}
	}
}
