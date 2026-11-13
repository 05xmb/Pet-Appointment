package mysqlstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
)

func bookingFixture(t *testing.T) (*Store, context.Context, time.Time) {
	t.Helper()
	db, _ := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 8, 15, 0, 0, time.UTC)
	if err := store.SeedTeachingData(ctx, now); err != nil {
		t.Fatal(err)
	}
	return store, ctx, now
}

func TestBookingCreateRejectsDuplicateCapacityAndStartedSlot(t *testing.T) {
	store, ctx, now := bookingFixture(t)
	first, err := store.CreatePending(ctx, 1, 1, 1, now)
	if err != nil || first.Status != booking.StatusPendingPayment || !first.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	if _, err := store.CreatePending(ctx, 1, 1, 1, now); !errors.Is(err, booking.ErrDuplicatePet) {
		t.Fatalf("duplicate error=%v", err)
	}
	for _, ownerID := range []int64{2, 3} {
		if _, err := store.CreatePending(ctx, ownerID, ownerID, 1, now); err != nil {
			t.Fatalf("owner %d: %v", ownerID, err)
		}
	}
	if _, err := store.CreatePending(ctx, 4, 4, 1, now); !errors.Is(err, booking.ErrCapacityFull) {
		t.Fatalf("capacity error=%v", err)
	}
	if _, err := store.CreatePending(ctx, 4, 4, 1, now.Add(4*time.Hour)); !errors.Is(err, booking.ErrSlotStarted) {
		t.Fatalf("started slot error=%v", err)
	}
}

func TestBookingTimeoutAndCancelReleaseExactlyOnce(t *testing.T) {
	store, ctx, now := bookingFixture(t)
	created, err := store.CreatePending(ctx, 1, 1, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := store.CloseExpired(ctx, created.ID, now.Add(5*time.Minute))
	if err != nil || closed.Status != booking.StatusClosed {
		t.Fatalf("closed=%+v error=%v", closed, err)
	}
	repeated, err := store.CloseExpired(ctx, created.ID, now.Add(6*time.Minute))
	if err != nil || repeated.Status != booking.StatusClosed {
		t.Fatalf("repeated close=%+v error=%v", repeated, err)
	}
	if _, err := store.CreatePending(ctx, 2, 2, 1, now.Add(6*time.Minute)); err != nil {
		t.Fatalf("released slot was not reusable: %v", err)
	}
	created, err = store.CreatePending(ctx, 3, 3, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.CancelPending(ctx, created.ID, 3)
	if err != nil || cancelled.Status != booking.StatusCancelled {
		t.Fatalf("cancelled=%+v error=%v", cancelled, err)
	}
	repeated, err = store.CancelPending(ctx, created.ID, 3)
	if err != nil || repeated.Status != booking.StatusCancelled {
		t.Fatalf("repeated cancel=%+v error=%v", repeated, err)
	}
	if _, err := store.CreatePending(ctx, 4, 4, 2, now); err != nil {
		t.Fatalf("cancelled slot was not reusable: %v", err)
	}
}

func TestBookingConcurrentLastSlotOnlyOneSucceeds(t *testing.T) {
	store, ctx, now := bookingFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for ownerID := int64(1); ownerID <= 4; ownerID++ {
		ownerID := ownerID
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.CreatePending(ctx, ownerID, ownerID, 1, now)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, booking.ErrCapacityFull) {
			t.Fatalf("unexpected concurrent error=%v", err)
		}
	}
	if successes != 3 {
		t.Fatalf("successful bookings=%d, want 3", successes)
	}
}
