package mysqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/refund"
)

func TestBookingAndPaymentRejectMissingForeignOwnerAndInvalidStates(t *testing.T) {
	store, ctx, now := bookingFixture(t)

	if _, err := store.CreatePending(ctx, 1, 1, 99, now); !errors.Is(err, booking.ErrNotFound) {
		t.Fatalf("missing slot error=%v", err)
	}
	if _, err := store.CreatePending(ctx, 1, 99, 1, now); !errors.Is(err, booking.ErrPetNotFound) {
		t.Fatalf("missing pet error=%v", err)
	}
	if _, err := store.CreatePending(ctx, 2, 1, 1, now); !errors.Is(err, booking.ErrForbidden) {
		t.Fatalf("foreign pet error=%v", err)
	}
	if _, err := store.CloseExpired(ctx, 99, now); !errors.Is(err, booking.ErrNotFound) {
		t.Fatalf("missing close error=%v", err)
	}
	b, err := store.CreatePending(ctx, 1, 1, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseExpired(ctx, b.ID, now.Add(time.Minute)); !errors.Is(err, booking.ErrNotExpired) {
		t.Fatalf("early close error=%v", err)
	}
	if _, err := store.CancelPending(ctx, 2, b.ID); !errors.Is(err, booking.ErrForbidden) {
		t.Fatalf("foreign cancel error=%v", err)
	}
	if _, err := store.CancelPending(ctx, 1, 99); !errors.Is(err, booking.ErrNotFound) {
		t.Fatalf("missing cancel error=%v", err)
	}

	paymentRecord, err := store.CreatePayment(ctx, 1, b.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePayment(ctx, 2, b.ID, now); !errors.Is(err, booking.ErrForbidden) {
		t.Fatalf("foreign payment error=%v", err)
	}
	if _, err := store.CreatePayment(ctx, 1, 99, now); !errors.Is(err, booking.ErrNotFound) {
		t.Fatalf("missing payment booking error=%v", err)
	}
	if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 99, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now); !errors.Is(err, payment.ErrPaymentNotFound) {
		t.Fatalf("missing callback payment error=%v", err)
	}
	if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: paymentRecord.ID, BookingID: b.ID + 1, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now); !errors.Is(err, payment.ErrInvalidCallback) {
		t.Fatalf("wrong callback booking error=%v", err)
	}

	paid, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: paymentRecord.ID, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now)
	if err != nil || paid.Status != payment.StatusSuccess {
		t.Fatalf("paid=%+v error=%v", paid, err)
	}
	if _, err := store.CloseExpired(ctx, b.ID, now.Add(6*time.Minute)); !errors.Is(err, booking.ErrInvalidState) {
		t.Fatalf("close confirmed error=%v", err)
	}
	if _, err := store.CancelPending(ctx, 1, b.ID); !errors.Is(err, booking.ErrInvalidState) {
		t.Fatalf("cancel confirmed error=%v", err)
	}
	closed, err := store.CreatePending(ctx, 2, 2, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CancelPending(ctx, 2, closed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseExpired(ctx, closed.ID, now.Add(6*time.Minute)); !errors.Is(err, booking.ErrInvalidState) {
		t.Fatalf("close cancelled error=%v", err)
	}
	expired, err := store.CreatePending(ctx, 3, 3, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseExpired(ctx, expired.ID, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePayment(ctx, 3, expired.ID, now.Add(5*time.Minute)); !errors.Is(err, payment.ErrBookingNotPayable) {
		t.Fatalf("closed payment error=%v", err)
	}
}

func TestCancellationBoundariesReviewRejectionAndRefundErrors(t *testing.T) {
	store, ctx, now, b, p := paidBookingFixture(t)

	if _, err := store.CancelConfirmed(ctx, 2, b.ID, now); !errors.Is(err, cancellation.ErrForbidden) {
		t.Fatalf("foreign direct cancel error=%v", err)
	}
	if _, err := store.CancelConfirmed(ctx, 1, b.ID, now.Add(3*time.Hour)); !errors.Is(err, cancellation.ErrTooLate) {
		t.Fatalf("late direct cancel error=%v", err)
	}
	if _, err := store.RequestCancellation(ctx, 1, b.ID, " ", now.Add(3*time.Hour)); !errors.Is(err, cancellation.ErrInvalidState) {
		t.Fatalf("blank reason error=%v", err)
	}
	if _, err := store.RequestCancellation(ctx, 1, 99, "临时有事", now.Add(3*time.Hour)); !errors.Is(err, booking.ErrNotFound) {
		t.Fatalf("missing request booking error=%v", err)
	}
	if _, err := store.RequestCancellation(ctx, 2, b.ID, "临时有事", now.Add(3*time.Hour)); !errors.Is(err, cancellation.ErrForbidden) {
		t.Fatalf("foreign request error=%v", err)
	}
	if _, err := store.RequestCancellation(ctx, 1, b.ID, "提前", now); !errors.Is(err, cancellation.ErrTooEarly) {
		t.Fatalf("early request error=%v", err)
	}

	request, err := store.RequestCancellation(ctx, 1, b.ID, "行程变化", now.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReviewCancellation(ctx, 99, request.ID, false, now.Add(3*time.Hour)); !errors.Is(err, cancellation.ErrForbidden) {
		t.Fatalf("unknown reviewer error=%v", err)
	}
	if _, err := store.ReviewCancellation(ctx, 1, request.ID, false, now.Add(3*time.Hour)); !errors.Is(err, cancellation.ErrForbidden) {
		t.Fatalf("owner reviewer error=%v", err)
	}
	restored, err := store.ReviewCancellation(ctx, 7, request.ID, false, now.Add(3*time.Hour))
	if err != nil || restored.Status != booking.StatusConfirmed {
		t.Fatalf("rejected=%+v error=%v", restored, err)
	}
	if _, err := store.ReviewCancellation(ctx, 7, request.ID, true, now.Add(3*time.Hour)); !errors.Is(err, cancellation.ErrInvalidState) {
		t.Fatalf("repeat review error=%v", err)
	}
	if _, err := store.ReviewCancellation(ctx, 7, 99, true, now.Add(3*time.Hour)); !errors.Is(err, booking.ErrNotFound) {
		t.Fatalf("missing review error=%v", err)
	}

	if _, err := store.ApplyRefundResult(ctx, 99, true); !errors.Is(err, cancellation.ErrInvalidState) {
		t.Fatalf("missing refund error=%v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO refunds (booking_id, payment_id, amount_cents, status) VALUES (?, ?, ?, 'BOGUS')`, b.ID, p.ID, 9900); err != nil {
		t.Fatal(err)
	}
	var refundID int64
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM refunds WHERE booking_id = ?`, b.ID).Scan(&refundID); err != nil {
		t.Fatal(err)
	}
	unknown, err := store.ApplyRefundResult(ctx, refundID, true)
	if err != nil || unknown.Status != refund.Status("BOGUS") {
		t.Fatalf("unknown refund=%+v error=%v", unknown, err)
	}
}

func TestCancellationWithoutSuccessfulPaymentIsRejected(t *testing.T) {
	store, ctx, now := bookingFixture(t)
	b, err := store.CreatePending(ctx, 1, 1, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE bookings SET status = 'CONFIRMED' WHERE id = ?`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CancelConfirmed(ctx, 1, b.ID, now); !errors.Is(err, payment.ErrBookingNotPayable) {
		t.Fatalf("unpaid direct cancel error=%v", err)
	}
}

func TestFulfillmentRejectsMissingRoleStoreWindowAndState(t *testing.T) {
	store, ctx, now, b := paidBookingFor(t, 1, 1, 1)

	if _, _, err := store.CheckIn(ctx, 5, 99, now.Add(3*time.Hour+30*time.Minute)); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("missing check-in booking error=%v", err)
	}
	if _, _, err := store.CheckIn(ctx, 99, b.ID, now.Add(3*time.Hour+30*time.Minute)); !errors.Is(err, fulfillment.ErrForbidden) {
		t.Fatalf("unknown check-in staff error=%v", err)
	}
	if _, _, err := store.CheckIn(ctx, 5, b.ID, now.Add(5*time.Hour+time.Minute)); !errors.Is(err, fulfillment.ErrOutsideCheckInWindow) {
		t.Fatalf("late check-in error=%v", err)
	}
	if _, _, err := store.StartService(ctx, 99, b.ID, now.Add(4*time.Hour)); !errors.Is(err, fulfillment.ErrForbidden) {
		t.Fatalf("unknown worker error=%v", err)
	}
	if _, _, err := store.StartService(ctx, 6, b.ID, now.Add(5*time.Hour+time.Minute)); !errors.Is(err, fulfillment.ErrInvalidState) {
		t.Fatalf("late service start error=%v", err)
	}
	if _, _, err := store.CompleteService(ctx, 6, 99, now.Add(5*time.Hour)); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("missing completion booking error=%v", err)
	}
	if _, _, err := store.MarkNoShow(ctx, 8, b.ID, " ", now.Add(5*time.Hour+time.Minute)); !errors.Is(err, fulfillment.ErrReasonRequired) {
		t.Fatalf("blank no-show reason error=%v", err)
	}
	if _, _, err := store.MarkNoShow(ctx, 99, b.ID, "未到店", now.Add(5*time.Hour+time.Minute)); !errors.Is(err, fulfillment.ErrForbidden) {
		t.Fatalf("unknown manager error=%v", err)
	}

	if _, err := store.db.ExecContext(ctx, `INSERT INTO stores (id, name) VALUES (2, '其他门店')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE slots SET store_id = 2 WHERE id = ?`, b.SlotID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CheckIn(ctx, 5, b.ID, now.Add(3*time.Hour+30*time.Minute)); !errors.Is(err, fulfillment.ErrForbidden) {
		t.Fatalf("cross-store check-in error=%v", err)
	}
	if _, _, err := store.StartService(ctx, 6, b.ID, now.Add(4*time.Hour)); !errors.Is(err, fulfillment.ErrForbidden) {
		t.Fatalf("cross-store start error=%v", err)
	}
	if _, _, err := store.MarkNoShow(ctx, 8, b.ID, "未到店", now.Add(5*time.Hour+time.Minute)); !errors.Is(err, fulfillment.ErrForbidden) {
		t.Fatalf("cross-store no-show error=%v", err)
	}
}

func TestFulfillmentInvalidStatusTransitions(t *testing.T) {
	store, ctx, now, b := paidBookingFor(t, 1, 1, 1)
	if _, _, err := store.CompleteService(ctx, 6, b.ID, now.Add(4*time.Hour)); !errors.Is(err, fulfillment.ErrInvalidState) {
		t.Fatalf("complete before check-in error=%v", err)
	}
	if _, _, err := store.MarkNoShow(ctx, 8, b.ID, "未到店", now.Add(5*time.Hour+time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.StartService(ctx, 6, b.ID, now.Add(4*time.Hour)); !errors.Is(err, fulfillment.ErrInvalidState) {
		t.Fatalf("start no-show error=%v", err)
	}
}

func TestFulfillmentEventConstraintRollsBackStatusChange(t *testing.T) {
	store, ctx, now, b := paidBookingFor(t, 1, 1, 1)
	if _, _, err := store.CheckIn(ctx, 5, b.ID, now.Add(3*time.Hour+30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE bookings SET status = 'CONFIRMED' WHERE id = ?`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CheckIn(ctx, 5, b.ID, now.Add(3*time.Hour+31*time.Minute)); err == nil {
		t.Fatal("duplicate check-in event unexpectedly succeeded")
	}
	var status string
	if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(booking.StatusConfirmed) {
		t.Fatalf("status=%s, want rollback to confirmed", status)
	}
}

func TestClosedDatabaseRejectsTransactionalBusinessOperations(t *testing.T) {
	db, err := openClosedTestDB()
	if err != nil {
		t.Fatal(err)
	}
	store := New(db)
	ctx := context.Background()
	now := time.Now()
	checks := []struct {
		name string
		call func() error
	}{
		{"create pending", func() error { _, err := store.CreatePending(ctx, 1, 1, 1, now); return err }},
		{"close expired", func() error { _, err := store.CloseExpired(ctx, 1, now); return err }},
		{"cancel pending", func() error { _, err := store.CancelPending(ctx, 1, 1); return err }},
		{"create payment", func() error { _, err := store.CreatePayment(ctx, 1, 1, now); return err }},
		{"apply callback", func() error {
			_, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 1, BookingID: 1, AmountCents: 9900}, now)
			return err
		}},
		{"direct cancel", func() error { _, err := store.CancelConfirmed(ctx, 1, 1, now); return err }},
		{"request cancellation", func() error { _, err := store.RequestCancellation(ctx, 1, 1, "reason", now); return err }},
		{"review cancellation", func() error { _, err := store.ReviewCancellation(ctx, 7, 1, true, now); return err }},
		{"refund result", func() error { _, err := store.ApplyRefundResult(ctx, 1, true); return err }},
		{"check-in", func() error { _, _, err := store.CheckIn(ctx, 5, 1, now); return err }},
		{"start service", func() error { _, _, err := store.StartService(ctx, 6, 1, now); return err }},
		{"complete service", func() error { _, _, err := store.CompleteService(ctx, 6, 1, now); return err }},
		{"no-show", func() error { _, _, err := store.MarkNoShow(ctx, 8, 1, "reason", now); return err }},
		{"list bookings", func() error {
			_, err := store.ListBookings(ctx, auth.Identity{StoreID: 1, Role: "manager"}, now, now.Add(time.Hour))
			return err
		}},
		{"daily summary", func() error {
			_, err := store.DailySummary(ctx, 1, now, now.Add(time.Hour), "2026-11-13")
			return err
		}},
	}
	for _, check := range checks {
		if err := check.call(); err == nil {
			t.Errorf("%s unexpectedly succeeded", check.name)
		}
	}
}

func TestSessionAndSeedRejectUnexpectedIdentityData(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.SeedTeachingData(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("missing-account"))
	if err := store.SaveSession(ctx, digest, 99, "hash", time.Now().Add(time.Hour)); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("unknown account session error=%v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, username, password_hash, role, store_id, owner_id) VALUES (9, 'unexpected', '', 'owner', 1, 9)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnconfiguredAccounts(ctx); err == nil {
		t.Fatal("unexpected teaching account was accepted")
	}
}

func openClosedTestDB() (*sql.DB, error) {
	db, err := sql.Open("mysql", "user@tcp(127.0.0.1:3306)/unused")
	if err != nil {
		return nil, err
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return db, nil
}
