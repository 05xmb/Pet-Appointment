package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardPageProvidesLoginQueriesAndActions(t *testing.T) {
	w := httptest.NewRecorder()
	serveDashboard(w, httptest.NewRequest(http.MethodGet, "https://example.test/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "loginForm") || !strings.Contains(w.Body.String(), "/daily-summary") || !strings.Contains(w.Body.String(), "service/complete") {
		t.Fatalf("dashboard status=%d body=%q", w.Code, w.Body.String())
	}
	notFound := httptest.NewRecorder()
	serveDashboard(notFound, httptest.NewRequest(http.MethodGet, "https://example.test/other", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("dashboard subpath status=%d", notFound.Code)
	}
}

func TestDashboardExposesEveryRoleWorkflow(t *testing.T) {
	w := httptest.NewRecorder()
	serveDashboard(w, httptest.NewRequest(http.MethodGet, "https://example.test/", nil))
	body := w.Body.String()
	for _, marker := range []string{
		"slotForm", "bookingForm", "paymentForm", "reviewForm", "refundForm",
		"logout", "/slots", "/bookings", "/payments", "/cancellation-requests/",
		"/refunds/callback", "check-in", "service/start", "service/complete", "no-show",
		"renderTable", "daily-summary",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("dashboard missing %q", marker)
		}
	}
}
