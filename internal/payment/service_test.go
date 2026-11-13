package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	created     Payment
	callback    Payment
	createErr   error
	callbackErr error
	gotCallback Callback
}

func (r *fakeRepository) CreatePayment(_ context.Context, _ int64, _ int64, _ time.Time) (Payment, error) {
	if r.createErr != nil {
		return Payment{}, r.createErr
	}
	return r.created, nil
}

func (r *fakeRepository) ApplyCallback(_ context.Context, callback Callback, _ time.Time) (Payment, error) {
	r.gotCallback = callback
	if r.callbackErr != nil {
		return Payment{}, r.callbackErr
	}
	return r.callback, nil
}

func TestServiceVerifiesRawHMACBeforeApplyingCallback(t *testing.T) {
	repo := &fakeRepository{callback: Payment{ID: 8, Status: StatusSuccess, AmountCents: 9900}}
	svc := NewService(repo, []byte("test-secret"))
	raw := []byte(`{"payment_id":8,"booking_id":3,"outcome":"success","amount_cents":9900}`)
	callback, err := svc.Callback(context.Background(), raw, Sign([]byte("test-secret"), raw), time.Now())
	if err != nil || callback.ID != 8 || repo.gotCallback.AmountCents != 9900 {
		t.Fatalf("callback=%+v got=%+v error=%v", callback, repo.gotCallback, err)
	}
	if _, err := svc.Callback(context.Background(), raw, "bad-signature", time.Now()); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("invalid signature error=%v", err)
	}
}

func TestServicePreservesPaymentErrors(t *testing.T) {
	repo := &fakeRepository{createErr: ErrBookingNotPayable}
	_, err := NewService(repo, []byte("secret")).CreatePayment(context.Background(), 7, 3, time.Now())
	if !errors.Is(err, ErrBookingNotPayable) {
		t.Fatalf("create error=%v", err)
	}
	repo = &fakeRepository{callbackErr: ErrPaymentAmountMismatch}
	raw := []byte(`{"payment_id":8,"booking_id":3,"outcome":"success","amount_cents":1}`)
	_, err = NewService(repo, []byte("secret")).Callback(context.Background(), raw, Sign([]byte("secret"), raw), time.Now())
	if !errors.Is(err, ErrPaymentAmountMismatch) {
		t.Fatalf("callback error=%v", err)
	}
}
