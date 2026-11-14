package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/payment"
)

func TestCancellationRequestAuditFailureReleasesTransactionConnection(t *testing.T) {
	store, ctx, now, b, _ := paidBookingFixture(t)
	store.db.SetMaxOpenConns(1)
	if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
		t.Fatal(err)
	}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	if _, err := store.RequestCancellation(requestCtx, 1, b.ID, "行程变化", now.Add(3*time.Hour)); err == nil {
		t.Fatal("request succeeded without audit log")
	}

	queryCtx, cancelQuery := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelQuery()
	var one int
	err := store.db.QueryRowContext(queryCtx, `SELECT 1`).Scan(&one)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("transaction connection was not released: %v", err)
	}
	if err != nil || one != 1 {
		t.Fatalf("query result=%d error=%v", one, err)
	}
}

func TestSlotCreationWritesAuditLogInSameTransaction(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	if err := store.SeedTeachingData(ctx, base); err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(ctx, 8, 1, 1, base.Add(8*time.Hour), base.Add(9*time.Hour), 2)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'slot' AND entity_id = ? AND action = 'created'`, created.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("audit count=%d", count)
	}
}

func TestAuditWriteFailureRollsBackBusinessState(t *testing.T) {
	t.Run("slot", func(t *testing.T) {
		db, _ := testDatabase(t)
		ctx := context.Background()
		store := New(db)
		base := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
		if err := store.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if err := store.SeedTeachingData(ctx, base); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Create(ctx, 8, 1, 1, base.Add(8*time.Hour), base.Add(9*time.Hour), 2); err == nil {
			t.Fatal("slot succeeded without audit log")
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM slots`).Scan(&count); err != nil || count != 3 {
			t.Fatalf("slots=%d error=%v", count, err)
		}
	})

	t.Run("booking", func(t *testing.T) {
		store, ctx, now := bookingFixture(t)
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); err == nil {
			t.Fatal("booking succeeded without audit log")
		}
		var count int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bookings`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("bookings=%d error=%v", count, err)
		}
	})

	t.Run("payment callback", func(t *testing.T) {
		store, ctx, now := bookingFixture(t)
		b, err := store.CreatePending(ctx, 1, 1, 1, now)
		if err != nil {
			t.Fatal(err)
		}
		p, err := store.CreatePayment(ctx, 1, b.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: p.ID, BookingID: b.ID, AmountCents: 9900, Outcome: payment.OutcomeSuccess}, now); err == nil {
			t.Fatal("payment callback succeeded without audit log")
		}
		var paymentStatus, bookingStatus string
		if err := store.db.QueryRowContext(ctx, `SELECT p.status, b.status FROM payments p JOIN bookings b ON b.id = p.booking_id WHERE p.id = ?`, p.ID).Scan(&paymentStatus, &bookingStatus); err != nil {
			t.Fatal(err)
		}
		if paymentStatus != "PENDING" || bookingStatus != string(booking.StatusPendingPayment) {
			t.Fatalf("payment=%s booking=%s", paymentStatus, bookingStatus)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		store, ctx, now, b, _ := paidBookingFixture(t)
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CancelConfirmed(ctx, 1, b.ID, now); err == nil {
			t.Fatal("cancellation succeeded without audit log")
		}
		var status string
		var refunds int
		if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds WHERE booking_id = ?`, b.ID).Scan(&refunds); err != nil {
			t.Fatal(err)
		}
		if status != string(booking.StatusConfirmed) || refunds != 0 {
			t.Fatalf("status=%s refunds=%d", status, refunds)
		}
	})

	t.Run("fulfillment", func(t *testing.T) {
		store, ctx, now, b := paidBookingFor(t, 1, 1, 1)
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.CheckIn(ctx, 5, b.ID, now.Add(3*time.Hour+30*time.Minute)); err == nil {
			t.Fatal("check-in succeeded without audit log")
		}
		var status string
		var events int
		if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_events WHERE booking_id = ?`, b.ID).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if status != string(booking.StatusConfirmed) || events != 0 {
			t.Fatalf("status=%s events=%d", status, events)
		}
	})

	t.Run("pending cancellation", func(t *testing.T) {
		store, ctx, now := bookingFixture(t)
		b, err := store.CreatePending(ctx, 1, 1, 1, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CancelPending(ctx, 1, b.ID); err == nil {
			t.Fatal("pending cancellation succeeded without audit log")
		}
		var status string
		if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&status); err != nil || status != string(booking.StatusPendingPayment) {
			t.Fatalf("status=%s error=%v", status, err)
		}
	})

	t.Run("expired booking", func(t *testing.T) {
		store, ctx, now := bookingFixture(t)
		b, err := store.CreatePending(ctx, 1, 1, 1, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CloseExpired(ctx, b.ID, now.Add(5*time.Minute)); err == nil {
			t.Fatal("expiration succeeded without audit log")
		}
		var status string
		if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&status); err != nil || status != string(booking.StatusPendingPayment) {
			t.Fatalf("status=%s error=%v", status, err)
		}
	})

	t.Run("payment creation", func(t *testing.T) {
		store, ctx, now := bookingFixture(t)
		b, err := store.CreatePending(ctx, 1, 1, 1, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreatePayment(ctx, 1, b.ID, now); err == nil {
			t.Fatal("payment creation succeeded without audit log")
		}
		var count int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE booking_id = ?`, b.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("payments=%d error=%v", count, err)
		}
	})

	t.Run("cancellation request", func(t *testing.T) {
		store, ctx, now, b, _ := paidBookingFixture(t)
		late := now.Add(3 * time.Hour)
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.RequestCancellation(ctx, 1, b.ID, "行程变化", late); err == nil {
			t.Fatal("request succeeded without audit log")
		}
		var status string
		var requests int
		if err := store.db.QueryRowContext(ctx, `SELECT status FROM bookings WHERE id = ?`, b.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cancellation_requests WHERE booking_id = ?`, b.ID).Scan(&requests); err != nil {
			t.Fatal(err)
		}
		if status != string(booking.StatusConfirmed) || requests != 0 {
			t.Fatalf("status=%s requests=%d", status, requests)
		}
	})

	t.Run("refund callback", func(t *testing.T) {
		store, ctx, now, b, _ := paidBookingFixture(t)
		if _, err := store.CancelConfirmed(ctx, 1, b.ID, now); err != nil {
			t.Fatal(err)
		}
		var refundID int64
		if err := store.db.QueryRowContext(ctx, `SELECT id FROM refunds WHERE booking_id = ?`, b.ID).Scan(&refundID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ApplyRefundResult(ctx, refundID, true); err == nil {
			t.Fatal("refund callback succeeded without audit log")
		}
		var status string
		if err := store.db.QueryRowContext(ctx, `SELECT status FROM refunds WHERE id = ?`, refundID).Scan(&status); err != nil || status != "PENDING" {
			t.Fatalf("status=%s error=%v", status, err)
		}
	})

	t.Run("review", func(t *testing.T) {
		store, ctx, now, b, _ := paidBookingFixture(t)
		request, err := store.RequestCancellation(ctx, 1, b.ID, "行程变化", now.Add(3*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `DROP TABLE audit_logs`); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReviewCancellation(ctx, 7, request.ID, false, now.Add(3*time.Hour)); err == nil {
			t.Fatal("review succeeded without audit log")
		}
		var bookingStatus, requestStatus string
		if err := store.db.QueryRowContext(ctx, `SELECT b.status, cr.status FROM bookings b JOIN cancellation_requests cr ON cr.booking_id = b.id WHERE b.id = ?`, b.ID).Scan(&bookingStatus, &requestStatus); err != nil {
			t.Fatal(err)
		}
		if bookingStatus != string(booking.StatusCancelReview) || requestStatus != "PENDING" {
			t.Fatalf("booking=%s request=%s", bookingStatus, requestStatus)
		}
	})
}
