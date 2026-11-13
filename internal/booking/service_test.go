package booking

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	booking   Booking
	createErr error
	cancelErr error
	closeErr  error
	gotOwner  int64
}

func (r *fakeRepository) CreatePending(_ context.Context, ownerID, _, _ int64, _ time.Time) (Booking, error) {
	r.gotOwner = ownerID
	if r.createErr != nil {
		return Booking{}, r.createErr
	}
	return r.booking, nil
}

func (r *fakeRepository) CancelPending(_ context.Context, ownerID, _ int64) (Booking, error) {
	r.gotOwner = ownerID
	if r.cancelErr != nil {
		return Booking{}, r.cancelErr
	}
	return r.booking, nil
}

func (r *fakeRepository) CloseExpired(_ context.Context, _ int64, _ time.Time) (Booking, error) {
	if r.closeErr != nil {
		return Booking{}, r.closeErr
	}
	return r.booking, nil
}

func TestServicePassesServerIdentityToRepository(t *testing.T) {
	repo := &fakeRepository{booking: Booking{ID: 9, OwnerID: 7, Status: StatusPendingPayment}}
	svc := NewService(repo)
	booking, err := svc.Book(context.Background(), 7, 3, 1, time.Now())
	if err != nil || booking.ID != 9 || repo.gotOwner != 7 {
		t.Fatalf("booking=%+v owner=%d error=%v", booking, repo.gotOwner, err)
	}
}

func TestServicePreservesBusinessErrors(t *testing.T) {
	for name, setup := range map[string]func(*fakeRepository){
		"duplicate": func(r *fakeRepository) { r.createErr = ErrDuplicatePet },
		"capacity":  func(r *fakeRepository) { r.createErr = ErrCapacityFull },
		"started":   func(r *fakeRepository) { r.createErr = ErrSlotStarted },
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepository{}
			setup(repo)
			_, err := NewService(repo).Book(context.Background(), 7, 3, 1, time.Now())
			if !errors.Is(err, repo.createErr) {
				t.Fatalf("error=%v, want %v", err, repo.createErr)
			}
		})
	}
}

func TestServiceCancelUsesOwnerIdentityAndCloseUsesBooking(t *testing.T) {
	repo := &fakeRepository{booking: Booking{ID: 9, OwnerID: 7, Status: StatusCancelled}}
	svc := NewService(repo)
	if _, err := svc.CancelUnpaid(context.Background(), 7, 9); err != nil || repo.gotOwner != 7 {
		t.Fatalf("cancel owner=%d error=%v", repo.gotOwner, err)
	}
	if _, err := svc.CloseExpired(context.Background(), 9, time.Now()); err != nil {
		t.Fatalf("close error=%v", err)
	}
}
