package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/refund"
)

func TestRefundCallbackRequiresSharedHMACAndSupportsRetryOutcome(t *testing.T) {
	repo := &cancellationRepository{refund: refund.Refund{ID: 9, Status: refund.StatusSuccess}}
	_, clock, accountRepo := testFixtureForRole(t, "support")
	h := NewHandler(accountService(accountRepo), clock, WithCancellationService(cancellation.NewServiceWithSecret(repo, []byte("shared-secret"))))
	raw := []byte(`{"refund_id":9,"outcome":"success"}`)
	call := func(signature string, body []byte) int {
		req := httptest.NewRequest(http.MethodPost, "https://example.test/refunds/callback", strings.NewReader(string(body)))
		req.Header.Set("X-Signature", signature)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	if got := call("bad", raw); got != http.StatusUnauthorized || repo.refundID != 0 {
		t.Fatalf("bad signature status=%d refundID=%d", got, repo.refundID)
	}
	if got := call(payment.Sign([]byte("shared-secret"), raw), raw); got != http.StatusOK || repo.refundID != 9 || !repo.refundOK {
		t.Fatalf("valid callback status=%d refundID=%d success=%v", got, repo.refundID, repo.refundOK)
	}
	if got := call(payment.Sign([]byte("shared-secret"), []byte(`{"refund_id":9,"outcome":"unknown"}`)), []byte(`{"refund_id":9,"outcome":"unknown"}`)); got != http.StatusBadRequest {
		t.Fatalf("invalid outcome status=%d", got)
	}
}
