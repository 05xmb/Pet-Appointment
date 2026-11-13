package slot

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repository struct {
	created   Slot
	createErr error
}

func (r *repository) Create(_ context.Context, _, _, _ int64, _, _ time.Time, _ int) (Slot, error) {
	if r.createErr != nil {
		return Slot{}, r.createErr
	}
	return r.created, nil
}

func (r *repository) List(context.Context, int64, time.Time, time.Time) ([]Slot, error) {
	return nil, nil
}

func TestCreateValidatesFutureWindowAndCapacity(t *testing.T) {
	now := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	r := &repository{created: Slot{ID: 9, Capacity: 3}}
	svc := NewService(r)
	start := now.Add(4 * time.Hour)
	if got, err := svc.Create(context.Background(), 8, 1, 1, start, start.Add(time.Hour), now, 3); err != nil || got.ID != 9 {
		t.Fatalf("valid slot=%+v error=%v", got, err)
	}
	for _, tc := range []struct {
		name       string
		start, end time.Time
		capacity   int
		want       error
	}{
		{"started", now, now.Add(time.Hour), 3, ErrInvalidTime},
		{"reversed", start.Add(time.Hour), start, 3, ErrInvalidTime},
		{"zero capacity", start, start.Add(time.Hour), 0, ErrInvalidCapacity},
		{"four capacity", start, start.Add(time.Hour), 4, ErrInvalidCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(context.Background(), 8, 1, 1, tc.start, tc.end, now, tc.capacity); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
		})
	}
}
