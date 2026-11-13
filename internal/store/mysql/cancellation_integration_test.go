package mysqlstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/refund"
)

func paidBookingFixture(t *testing.T) (*Store, context.Context, time.Time, booking.Booking, payment.Payment) {
	t.Helper()
	store, ctx, now, b := paymentFixture(t)
	p, err := store.CreatePayment(ctx, 1, b.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: p.ID, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now); err != nil {
		t.Fatal(err)
	}
	return store, ctx, now, b, p
}

func TestEarlyCancellationCreatesFullRefund(t *testing.T) {
	store, ctx, now, b, p := paidBookingFixture(t)
	cancelled, err := store.CancelConfirmed(ctx, 1, b.ID, now)
	if err != nil || cancelled.Status != booking.StatusCancelled {
		t.Fatalf("cancelled=%+v error=%v", cancelled, err)
	}
	var requestCount, refundCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cancellation_requests WHERE booking_id = ?`, b.ID).Scan(&requestCount); err != nil || requestCount != 0 {
		t.Fatalf("request count=%d error=%v", requestCount, err)
	}
	var amount int64
	var status string
	if err := store.db.QueryRowContext(ctx, `SELECT amount_cents, status FROM refunds WHERE booking_id = ? AND payment_id = ?`, b.ID, p.ID).Scan(&amount, &status); err != nil {
		t.Fatal(err)
	}
	if amount != 9900 || status != string(refund.StatusPending) {
		t.Fatalf("refund amount=%d status=%s", amount, status)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds WHERE booking_id = ?`, b.ID).Scan(&refundCount); err != nil || refundCount != 1 {
		t.Fatalf("refund count=%d error=%v", refundCount, err)
	}
	if _, err := store.CancelConfirmed(ctx, 1, b.ID, now); !errors.Is(err, cancellation.ErrInvalidState) {
		t.Fatalf("repeat direct cancel error=%v", err)
	}
}

func TestLateCancellationRequestApproveRejectAndRefundRetry(t *testing.T) {
	store, ctx, now, b, _ := paidBookingFixture(t)
	late := now.Add(3 * time.Hour)
	req, err := store.RequestCancellation(ctx, 1, b.ID, "临时有事", late)
	if err != nil || req.Status != cancellation.StatusPending {
		t.Fatalf("request=%+v error=%v", req, err)
	}
	var status string
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&status); err != nil || status != string(booking.StatusCancelReview) {
		t.Fatalf("booking status=%s error=%v", status, err)
	}
	if _, err := store.RequestCancellation(ctx, 1, b.ID, "重复", late); !errors.Is(err, cancellation.ErrInvalidState) {
		t.Fatalf("duplicate request error=%v", err)
	}
	if _, err := store.ReviewCancellation(ctx, 1, req.ID, true, late); !errors.Is(err, cancellation.ErrForbidden) {
		t.Fatalf("owner review error=%v", err)
	}
	approved, err := store.ReviewCancellation(ctx, 7, req.ID, true, late)
	if err != nil || approved.Status != booking.StatusCancelled {
		t.Fatalf("approved=%+v error=%v", approved, err)
	}
	var refundID int64
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM refunds WHERE booking_id = ?`, b.ID).Scan(&refundID); err != nil {
		t.Fatal(err)
	}
	failed, err := store.ApplyRefundResult(ctx, refundID, false)
	if err != nil || failed.Status != refund.StatusFailed {
		t.Fatalf("failed refund=%+v error=%v", failed, err)
	}
	success, err := store.ApplyRefundResult(ctx, refundID, true)
	if err != nil || success.Status != refund.StatusSuccess {
		t.Fatalf("success refund=%+v error=%v", success, err)
	}
	repeated, err := store.ApplyRefundResult(ctx, refundID, true)
	if err != nil || repeated.Status != refund.StatusSuccess {
		t.Fatalf("repeat refund=%+v error=%v", repeated, err)
	}
	var bookingStatus string
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&bookingStatus); err != nil || bookingStatus != string(booking.StatusCancelled) {
		t.Fatalf("booking revived status=%s error=%v", bookingStatus, err)
	}
}

func TestStartedUnverifiedCancellationIsAllowed(t *testing.T) {
	store, ctx, now, b, _ := paidBookingFixture(t)
	started := now.Add(4*time.Hour + time.Minute)
	if _, err := store.RequestCancellation(ctx, 1, b.ID, "宠物不舒服", started); err != nil {
		t.Fatal(err)
	}
}
