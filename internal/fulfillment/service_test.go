package fulfillment

import (
	"context"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
)

type serviceRepository struct{}

func (serviceRepository) CheckIn(context.Context, int64, int64, time.Time) (booking.Booking, Event, error) {
	return booking.Booking{ID: 1, Status: booking.StatusCheckedIn}, Event{Type: EventCheckIn}, nil
}
func (serviceRepository) StartService(context.Context, int64, int64, time.Time) (booking.Booking, Event, error) {
	return booking.Booking{ID: 1, Status: booking.StatusInService}, Event{Type: EventServiceStart}, nil
}
func (serviceRepository) CompleteService(context.Context, int64, int64, time.Time) (booking.Booking, Event, error) {
	return booking.Booking{ID: 1, Status: booking.StatusCompleted}, Event{Type: EventServiceDone}, nil
}
func (serviceRepository) MarkNoShow(context.Context, int64, int64, string, time.Time) (booking.Booking, Event, error) {
	return booking.Booking{ID: 1, Status: booking.StatusNoShow}, Event{Type: EventNoShow}, nil
}

func TestServiceDelegatesFulfillmentOperations(t *testing.T) {
	service := NewService(serviceRepository{})
	ctx := context.Background()
	now := time.Now()
	if got, event, err := service.CheckIn(ctx, 5, 1, now); err != nil || got.Status != booking.StatusCheckedIn || event.Type != EventCheckIn {
		t.Fatalf("check-in=%+v event=%+v error=%v", got, event, err)
	}
	if got, event, err := service.Start(ctx, 6, 1, now); err != nil || got.Status != booking.StatusInService || event.Type != EventServiceStart {
		t.Fatalf("start=%+v event=%+v error=%v", got, event, err)
	}
	if got, event, err := service.Complete(ctx, 6, 1, now); err != nil || got.Status != booking.StatusCompleted || event.Type != EventServiceDone {
		t.Fatalf("complete=%+v event=%+v error=%v", got, event, err)
	}
	if got, event, err := service.NoShow(ctx, 8, 1, "未到店", now); err != nil || got.Status != booking.StatusNoShow || event.Type != EventNoShow {
		t.Fatalf("no-show=%+v event=%+v error=%v", got, event, err)
	}
}
