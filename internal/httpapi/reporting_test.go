package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/reporting"
)

type reportingRepository struct {
	views      []reporting.BookingView
	summary    reporting.DailySummary
	identity   auth.Identity
	from       time.Time
	to         time.Time
	storeID    int64
	listErr    error
	summaryErr error
}

func (r *reportingRepository) ListBookings(_ context.Context, identity auth.Identity, from, to time.Time) ([]reporting.BookingView, error) {
	r.identity, r.from, r.to = identity, from, to
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.views, nil
}

func (r *reportingRepository) DailySummary(_ context.Context, storeID int64, from, to time.Time, date string) (reporting.DailySummary, error) {
	r.storeID, r.from, r.to = storeID, from, to
	r.summary.Date = date
	if r.summaryErr != nil {
		return reporting.DailySummary{}, r.summaryErr
	}
	return r.summary, nil
}

func TestReportingHTTPMapsRepositoryFailures(t *testing.T) {
	repo := &reportingRepository{listErr: errors.New("list failed"), summaryErr: errors.New("summary failed")}
	h, clock, accountRepo := testFixtureForRole(t, "manager")
	h = NewHandler(accountService(accountRepo), clock, WithReportingService(reporting.NewService(repo)))
	cookie := loginCookie(t, h)
	for _, target := range []string{"/bookings?date=2026-11-13", "/daily-summary?date=2026-11-13"} {
		req := httptest.NewRequest(http.MethodGet, "https://example.test"+target, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("target=%s status=%d body=%q", target, w.Code, w.Body.String())
		}
	}
}

func TestDailySummaryHTTPMapsForbiddenRepository(t *testing.T) {
	repo := &reportingRepository{summaryErr: reporting.ErrForbidden}
	h, clock, accountRepo := testFixtureForRole(t, "manager")
	h = NewHandler(accountService(accountRepo), clock, WithReportingService(reporting.NewService(repo)))
	cookie := loginCookie(t, h)
	req := httptest.NewRequest(http.MethodGet, "https://example.test/daily-summary?date=2026-11-13", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("forbidden summary status=%d body=%q", w.Code, w.Body.String())
	}
}

func loginCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loginRequest("https"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("login status=%d body=%q", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func TestBookingQueryUsesSessionAndShanghaiDate(t *testing.T) {
	repo := &reportingRepository{views: []reporting.BookingView{{ID: 41, OwnerID: 7, Status: booking.StatusConfirmed}}}
	h, clock, accountRepo := testFixtureForRole(t, "owner")
	h = NewHandler(accountService(accountRepo), clock, WithReportingService(reporting.NewService(repo)))
	cookie := loginCookie(t, h)
	req := httptest.NewRequest(http.MethodGet, "https://example.test/bookings?date=2026-11-13&owner_id=999", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || repo.identity.OwnerID != 7 || len(repo.views) != 1 {
		t.Fatalf("status=%d identity=%+v body=%q", w.Code, repo.identity, w.Body.String())
	}
	wantFrom := time.Date(2026, 11, 12, 16, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 11, 13, 16, 0, 0, 0, time.UTC)
	if !repo.from.Equal(wantFrom) || !repo.to.Equal(wantTo) {
		t.Fatalf("range from=%s to=%s", repo.from, repo.to)
	}
}

func TestDailySummaryRequiresManagerAndValidDate(t *testing.T) {
	repo := &reportingRepository{summary: reporting.DailySummary{BookingCount: 3}}
	h, clock, accountRepo := testFixtureForRole(t, "manager")
	h = NewHandler(accountService(accountRepo), clock, WithReportingService(reporting.NewService(repo)))
	cookie := loginCookie(t, h)
	req := httptest.NewRequest(http.MethodGet, "https://example.test/daily-summary?date=2026-11-13", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || repo.storeID != 1 || !strings.Contains(w.Body.String(), "booking_count") {
		t.Fatalf("status=%d store=%d body=%q", w.Code, repo.storeID, w.Body.String())
	}
	bad := httptest.NewRequest(http.MethodGet, "https://example.test/daily-summary?date=bad", nil)
	bad.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, bad)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad date status=%d body=%q", w.Code, w.Body.String())
	}

	owner, ownerClock, ownerRepo := testFixtureForRole(t, "owner")
	owner = NewHandler(accountService(ownerRepo), ownerClock, WithReportingService(reporting.NewService(&reportingRepository{})))
	ownerCookie := loginCookie(t, owner)
	forbidden := httptest.NewRequest(http.MethodGet, "https://example.test/daily-summary?date=2026-11-13", nil)
	forbidden.AddCookie(ownerCookie)
	w = httptest.NewRecorder()
	owner.ServeHTTP(w, forbidden)
	if w.Code != http.StatusForbidden {
		t.Fatalf("owner daily status=%d body=%q", w.Code, w.Body.String())
	}
}
