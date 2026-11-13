package slot

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidTime     = errors.New("slot start and end times are invalid")
	ErrInvalidCapacity = errors.New("slot capacity must be between 1 and 3")
	ErrForbidden       = errors.New("slot operation is not allowed")
	ErrNotFound        = errors.New("slot or project not found")
)

type Repository interface {
	Create(context.Context, int64, int64, int64, time.Time, time.Time, int) (Slot, error)
	List(context.Context, int64, time.Time, time.Time) ([]Slot, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) Create(ctx context.Context, actorID, storeID, projectID int64, start, end, now time.Time, capacity int) (Slot, error) {
	if actorID <= 0 || storeID <= 0 || projectID <= 0 || !start.Before(end) || !start.After(now) {
		return Slot{}, ErrInvalidTime
	}
	if capacity < 1 || capacity > 3 {
		return Slot{}, ErrInvalidCapacity
	}
	return s.repository.Create(ctx, actorID, storeID, projectID, start, end, capacity)
}

func (s *Service) List(ctx context.Context, storeID int64, from, to time.Time) ([]Slot, error) {
	return s.repository.List(ctx, storeID, from, to)
}
