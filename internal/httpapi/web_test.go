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
