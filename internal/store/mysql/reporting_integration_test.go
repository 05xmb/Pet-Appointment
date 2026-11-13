package mysqlstore

import (
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/refund"
	"github.com/05xmb/Pet-Appointment/internal/reporting"
)

func TestReportingScopesBookingsAndCalculatesDailyMoney(t *testing.T) {
	store, ctx, now := bookingFixture(t)
	b1, err := store.CreatePending(ctx, 1, 1, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := store.CreatePayment(ctx, 1, b1.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: p1.ID, BookingID: b1.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now); err != nil {
		t.Fatal(err)
	}
	b2, err := store.CreatePending(ctx, 2, 2, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := store.CreatePayment(ctx, 2, b2.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: p2.ID, BookingID: b2.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CancelConfirmed(ctx, 2, b2.ID, now); err != nil {
		t.Fatal(err)
	}
	var refundID int64
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM refunds WHERE booking_id = ?`, b2.ID).Scan(&refundID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyRefundResult(ctx, refundID, true); err != nil {
		t.Fatal(err)
	}
	b3, err := store.CreatePending(ctx, 3, 3, 3, now)
	if err != nil {
		t.Fatal(err)
	}

	service := reporting.NewService(store)
	from := now.Add(-time.Hour)
	to := now.Add(8 * time.Hour)
	ownerViews, err := service.ListBookings(ctx, auth.Identity{ID: 1, Role: "owner", StoreID: 1, OwnerID: 1}, from, to)
	if err != nil || len(ownerViews) != 1 || ownerViews[0].ID != b1.ID {
		t.Fatalf("owner views=%+v error=%v", ownerViews, err)
	}
	staffViews, err := service.ListBookings(ctx, auth.Identity{ID: 5, Role: "desk", StoreID: 1}, from, to)
	if err != nil || len(staffViews) != 3 {
		t.Fatalf("staff views=%+v error=%v", staffViews, err)
	}
	if staffViews[2].ID != b3.ID || staffViews[1].RefundStatus != string(refund.StatusSuccess) || staffViews[1].CancellationStatus != "" {
		t.Fatalf("staff facts=%+v", staffViews)
	}
	summary, err := service.DailySummary(ctx, auth.Identity{ID: 8, Role: "manager", StoreID: 1}, from, to, "2026-11-13")
	if err != nil {
		t.Fatal(err)
	}
	if summary.BookingCount != 3 || summary.StatusCounts[string(booking.StatusConfirmed)] != 1 || summary.StatusCounts[string(booking.StatusCancelled)] != 1 || summary.StatusCounts[string(booking.StatusPendingPayment)] != 1 {
		t.Fatalf("summary counts=%+v", summary)
	}
	if summary.CollectedCents != 19800 || summary.RefundedCents != 9900 || summary.PendingRefundCents != 0 || summary.OutstandingCents != 9900 {
		t.Fatalf("summary money=%+v", summary)
	}
}
