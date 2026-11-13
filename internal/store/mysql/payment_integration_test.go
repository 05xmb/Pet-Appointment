package mysqlstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/payment"
)

func paymentFixture(t *testing.T) (*Store, context.Context, time.Time, booking.Booking) {
	t.Helper()
	store, ctx, now := bookingFixture(t)
	b, err := store.CreatePending(ctx, 1, 1, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	return store, ctx, now, b
}

func TestPaymentSuccessFailureTimeoutAndDuplicateAreDurable(t *testing.T) {
	store, ctx, now, b := paymentFixture(t)
	created, err := store.CreatePayment(ctx, 1, b.ID, now)
	if err != nil || created.Status != payment.StatusPending || created.AmountCents != 9900 {
		t.Fatalf("created=%+v error=%v", created, err)
	}
	repeated, err := store.CreatePayment(ctx, 1, b.ID, now)
	if err != nil || repeated.ID != created.ID {
		t.Fatalf("repeat payment=%+v error=%v", repeated, err)
	}
	successRaw := payment.Callback{PaymentID: created.ID, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}
	paid, err := store.ApplyCallback(ctx, successRaw, now)
	if err != nil || paid.Status != payment.StatusSuccess {
		t.Fatalf("paid=%+v error=%v", paid, err)
	}
	duplicate, err := store.ApplyCallback(ctx, successRaw, now)
	if err != nil || duplicate.Status != payment.StatusSuccess {
		t.Fatalf("duplicate=%+v error=%v", duplicate, err)
	}
	var bookingStatus string
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&bookingStatus); err != nil || bookingStatus != string(booking.StatusConfirmed) {
		t.Fatalf("booking status=%s error=%v", bookingStatus, err)
	}

	failedBooking, err := store.CreatePending(ctx, 2, 2, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	failedPayment, err := store.CreatePayment(ctx, 2, failedBooking.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: failedPayment.ID, BookingID: failedBooking.ID, Outcome: payment.OutcomeFailed, AmountCents: 9900}, now)
	if err != nil || failed.Status != payment.StatusFailed {
		t.Fatalf("failed=%+v error=%v", failed, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, failedBooking.ID).Scan(&bookingStatus); err != nil || bookingStatus != string(booking.StatusPendingPayment) {
		t.Fatalf("failed booking status=%s error=%v", bookingStatus, err)
	}

	timedBooking, err := store.CreatePending(ctx, 3, 3, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	timedPayment, err := store.CreatePayment(ctx, 3, timedBooking.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	timed, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: timedPayment.ID, BookingID: timedBooking.ID, Outcome: payment.OutcomeTimeout, AmountCents: 9900}, now)
	if err != nil || timed.Status != payment.StatusTimeout {
		t.Fatalf("timed=%+v error=%v", timed, err)
	}
}

func TestLatePaymentAfterClosedBookingCreatesRefundException(t *testing.T) {
	store, ctx, now, b := paymentFixture(t)
	p, err := store.CreatePayment(ctx, 1, b.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseExpired(ctx, b.ID, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	paid, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: p.ID, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now.Add(6*time.Minute))
	if err != nil || paid.Status != payment.StatusSuccess {
		t.Fatalf("late paid=%+v error=%v", paid, err)
	}
	var bookingStatus, exceptionStatus, exceptionType string
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&bookingStatus); err != nil || bookingStatus != string(booking.StatusClosed) {
		t.Fatalf("late booking status=%s error=%v", bookingStatus, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT type, status FROM exceptions WHERE payment_id = ?`, p.ID).Scan(&exceptionType, &exceptionStatus); err != nil || exceptionType != "LATE_PAYMENT" || exceptionStatus != "NEEDS_REFUND" {
		t.Fatalf("exception type=%s status=%s error=%v", exceptionType, exceptionStatus, err)
	}
	var refundID, refundAmount int64
	var refundStatus string
	if err := store.db.QueryRowContext(ctx, `SELECT id, amount_cents, status FROM refunds WHERE booking_id = ? AND payment_id = ?`, b.ID, p.ID).Scan(&refundID, &refundAmount, &refundStatus); err != nil || refundAmount != 9900 || refundStatus != "PENDING" {
		t.Fatalf("refund id=%d amount=%d status=%s error=%v", refundID, refundAmount, refundStatus, err)
	}
	if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: p.ID, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM exceptions WHERE payment_id = ?`, p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("exception count=%d error=%v", count, err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds WHERE payment_id = ?`, p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("refund count=%d error=%v", count, err)
	}
	if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: p.ID, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 1}, now); !errors.Is(err, payment.ErrPaymentAmountMismatch) {
		t.Fatalf("amount mismatch error=%v", err)
	}
}

func TestPaymentAfterUnpaidExpiryClosesBookingAndIsRejected(t *testing.T) {
	store, ctx, now, b := paymentFixture(t)
	if _, err := store.CreatePayment(ctx, 1, b.ID, now.Add(5*time.Minute)); !errors.Is(err, payment.ErrBookingNotPayable) {
		t.Fatalf("expired payment error=%v", err)
	}
	var status string
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&status); err != nil || status != string(booking.StatusClosed) {
		t.Fatalf("expired booking status=%s error=%v", status, err)
	}
	var payments int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE booking_id = ?`, b.ID).Scan(&payments); err != nil || payments != 0 {
		t.Fatalf("expired payment count=%d error=%v", payments, err)
	}
}

func TestLatePaymentRollsBackWhenRefundCannotBeRecorded(t *testing.T) {
	store, ctx, now, b := paymentFixture(t)
	p, err := store.CreatePayment(ctx, 1, b.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseExpired(ctx, b.ID, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DROP TABLE refunds`); err != nil {
		t.Fatal(err)
	}
	callback := payment.Callback{PaymentID: p.ID, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}
	if _, err := store.ApplyCallback(ctx, callback, now.Add(6*time.Minute)); err == nil {
		t.Fatal("late payment succeeded without durable refund")
	}
	var paymentStatus string
	var exceptions int
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM payments WHERE id = ?`, p.ID).Scan(&paymentStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM exceptions WHERE payment_id = ?`, p.ID).Scan(&exceptions); err != nil {
		t.Fatal(err)
	}
	if paymentStatus != "PENDING" || exceptions != 0 {
		t.Fatalf("payment=%s exceptions=%d", paymentStatus, exceptions)
	}
}
