package cancellation

import (
	"context"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/refund"
)

type serviceRepository struct {
	request Request
	booking booking.Booking
	refund  refund.Refund
}

func (r *serviceRepository) CancelConfirmed(context.Context, int64, int64, time.Time) (booking.Booking, error) {
	return r.booking, nil
}
func (r *serviceRepository) RequestCancellation(context.Context, int64, int64, string, time.Time) (Request, error) {
	return r.request, nil
}
func (r *serviceRepository) ReviewCancellation(context.Context, int64, int64, bool, time.Time) (booking.Booking, error) {
	return r.booking, nil
}
func (r *serviceRepository) ApplyRefundResult(context.Context, int64, bool) (refund.Refund, error) {
	return r.refund, nil
}

func TestServiceDelegatesCancellationOperations(t *testing.T) {
	repo := &serviceRepository{request: Request{ID: 3, Status: StatusPending}, booking: booking.Booking{ID: 4}, refund: refund.Refund{ID: 5}}
	service := NewService(repo)
	ctx := context.Background()
	now := time.Now()
	if got, err := service.CancelConfirmed(ctx, 1, 2, now); err != nil || got.ID != 4 {
		t.Fatalf("cancel=%+v error=%v", got, err)
	}
	if got, err := service.Request(ctx, 1, 2, "reason", now); err != nil || got.ID != 3 {
		t.Fatalf("request=%+v error=%v", got, err)
	}
	if got, err := service.Review(ctx, 7, 3, true, now); err != nil || got.ID != 4 {
		t.Fatalf("review=%+v error=%v", got, err)
	}
	if got, err := service.RefundResult(ctx, 5, true); err != nil || got.ID != 5 {
		t.Fatalf("refund=%+v error=%v", got, err)
	}
}

func TestRefundCallbackValidatesSharedSignatureAndOutcome(t *testing.T) {
	repo := &serviceRepository{refund: refund.Refund{ID: 5, Status: refund.StatusSuccess}}
	service := NewServiceWithSecret(repo, []byte("secret"))
	raw := []byte(`{"refund_id":5,"outcome":"success"}`)
	if _, err := service.RefundCallback(context.Background(), raw, "bad"); err != ErrInvalidSignature {
		t.Fatalf("bad signature error=%v", err)
	}
	if got, err := service.RefundCallback(context.Background(), raw, payment.Sign([]byte("secret"), raw)); err != nil || got.ID != 5 {
		t.Fatalf("success callback=%+v error=%v", got, err)
	}
	bad := []byte(`{"refund_id":5,"outcome":"unknown"}`)
	if _, err := service.RefundCallback(context.Background(), bad, payment.Sign([]byte("secret"), bad)); err != ErrInvalidCallback {
		t.Fatalf("bad outcome error=%v", err)
	}
}
