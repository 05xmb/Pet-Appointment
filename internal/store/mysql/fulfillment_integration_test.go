package mysqlstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
	"github.com/05xmb/Pet-Appointment/internal/payment"
)

func paidBookingFor(t *testing.T, ownerID, petID, slotID int64) (*Store, context.Context, time.Time, booking.Booking) {
	t.Helper()
	store, ctx, now := bookingFixture(t)
	b, err := store.CreatePending(ctx, ownerID, petID, slotID, now)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.CreatePayment(ctx, ownerID, b.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: p.ID, BookingID: b.ID, Outcome: payment.OutcomeSuccess, AmountCents: 9900}, now); err != nil {
		t.Fatal(err)
	}
	return store, ctx, now, b
}

func TestCheckInWindowRoleAndPendingCancellation(t *testing.T) {
	store, ctx, now, b := paidBookingFor(t, 1, 1, 1)
	checked, _, err := store.CheckIn(ctx, 5, b.ID, now.Add(3*time.Hour+30*time.Minute))
	if err != nil || checked.Status != booking.StatusCheckedIn {
		t.Fatalf("checked=%+v error=%v", checked, err)
	}
	if _, _, err := store.CheckIn(ctx, 5, b.ID, now.Add(4*time.Hour)); !errors.Is(err, fulfillment.ErrInvalidState) {
		t.Fatalf("duplicate check-in error=%v", err)
	}

	store2, ctx2, now2, b2 := paidBookingFor(t, 2, 2, 2)
	if _, _, err := store2.CheckIn(ctx2, 5, b2.ID, now2.Add(4*time.Hour)); !errors.Is(err, fulfillment.ErrOutsideCheckInWindow) {
		t.Fatalf("early check-in error=%v", err)
	}
	if _, _, err := store.CheckIn(ctx, 1, b.ID, now.Add(3*time.Hour+30*time.Minute)); !errors.Is(err, fulfillment.ErrForbidden) {
		t.Fatalf("owner check-in error=%v", err)
	}

	store3, ctx3, now3, b3 := paidBookingFor(t, 3, 3, 3)
	if _, err := store3.RequestCancellation(ctx3, 3, b3.ID, "临时有事", now3.Add(5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store3.CheckIn(ctx3, 5, b3.ID, now3.Add(5*time.Hour+15*time.Minute)); !errors.Is(err, fulfillment.ErrInvalidState) {
		t.Fatalf("pending cancellation check-in error=%v", err)
	}
}

func TestServiceLifecycleAllowsCompletionAfterEnd(t *testing.T) {
	store, ctx, now, b := paidBookingFor(t, 1, 1, 1)
	if _, _, err := store.CheckIn(ctx, 5, b.ID, now.Add(3*time.Hour+30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	started, _, err := store.StartService(ctx, 6, b.ID, now.Add(4*time.Hour))
	if err != nil || started.Status != booking.StatusInService {
		t.Fatalf("started=%+v error=%v", started, err)
	}
	if _, _, err := store.StartService(ctx, 6, b.ID, now.Add(4*time.Hour+10*time.Minute)); !errors.Is(err, fulfillment.ErrInvalidState) {
		t.Fatalf("duplicate start error=%v", err)
	}
	completed, _, err := store.CompleteService(ctx, 6, b.ID, now.Add(5*time.Hour+time.Minute))
	if err != nil || completed.Status != booking.StatusCompleted {
		t.Fatalf("completed=%+v error=%v", completed, err)
	}
	if _, _, err := store.CompleteService(ctx, 6, b.ID, now.Add(5*time.Hour+2*time.Minute)); !errors.Is(err, fulfillment.ErrInvalidState) {
		t.Fatalf("duplicate completion error=%v", err)
	}
	var events int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_events WHERE booking_id = ?`, b.ID).Scan(&events); err != nil || events != 3 {
		t.Fatalf("service event count=%d error=%v", events, err)
	}
}

func TestManagerCanMarkNoShowOnlyAfterEnd(t *testing.T) {
	store, ctx, now, b := paidBookingFor(t, 1, 1, 1)
	if _, _, err := store.MarkNoShow(ctx, 8, b.ID, "未到店", now.Add(4*time.Hour+30*time.Minute)); !errors.Is(err, fulfillment.ErrTooEarly) {
		t.Fatalf("early no-show error=%v", err)
	}
	if _, _, err := store.MarkNoShow(ctx, 1, b.ID, "未到店", now.Add(5*time.Hour+time.Minute)); !errors.Is(err, fulfillment.ErrForbidden) {
		t.Fatalf("owner no-show error=%v", err)
	}
	marked, event, err := store.MarkNoShow(ctx, 8, b.ID, "宠物未到店", now.Add(5*time.Hour+time.Minute))
	if err != nil || marked.Status != booking.StatusNoShow || event.Reason != "宠物未到店" {
		t.Fatalf("marked=%+v event=%+v error=%v", marked, event, err)
	}
	if _, _, err := store.MarkNoShow(ctx, 8, b.ID, "重复", now.Add(5*time.Hour+2*time.Minute)); !errors.Is(err, fulfillment.ErrInvalidState) {
		t.Fatalf("duplicate no-show error=%v", err)
	}
	if _, err := store.RequestCancellation(ctx, 1, b.ID, "迟到申请", now.Add(5*time.Hour+time.Minute)); !errors.Is(err, cancellation.ErrInvalidState) {
		t.Fatalf("no-show cancellation error=%v", err)
	}
}

func TestCancellationAndNoShowCompeteWithOneWinner(t *testing.T) {
	store, ctx, now, b := paidBookingFor(t, 1, 1, 1)
	when := now.Add(5*time.Hour + time.Minute)
	var wg sync.WaitGroup
	wg.Add(2)
	requestErr := make(chan error, 1)
	noShowErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		_, err := store.RequestCancellation(ctx, 1, b.ID, "临时有事", when)
		requestErr <- err
	}()
	go func() {
		defer wg.Done()
		_, _, err := store.MarkNoShow(ctx, 8, b.ID, "未到店", when)
		noShowErr <- err
	}()
	wg.Wait()
	requestResult := <-requestErr
	noShowResult := <-noShowErr
	if (requestResult == nil) == (noShowResult == nil) {
		t.Fatalf("expected exactly one winner: request=%v no_show=%v", requestResult, noShowResult)
	}
}

func TestCancellationAndCheckInCompeteWithOneWinner(t *testing.T) {
	store, ctx, now, b := paidBookingFor(t, 1, 1, 1)
	when := now.Add(4 * time.Hour)
	var wg sync.WaitGroup
	wg.Add(2)
	requestErr := make(chan error, 1)
	checkInErr := make(chan error, 1)
	go func() {
		defer wg.Done()
		_, err := store.RequestCancellation(ctx, 1, b.ID, "临时有事", when)
		requestErr <- err
	}()
	go func() {
		defer wg.Done()
		_, _, err := store.CheckIn(ctx, 5, b.ID, when)
		checkInErr <- err
	}()
	wg.Wait()
	requestResult := <-requestErr
	checkInResult := <-checkInErr
	if (requestResult == nil) == (checkInResult == nil) {
		t.Fatalf("expected exactly one winner: request=%v check-in=%v", requestResult, checkInResult)
	}
}
