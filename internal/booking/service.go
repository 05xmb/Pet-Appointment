package booking

import (
	"context"
	"errors"
	"time"
)

var (
	ErrDuplicatePet = errors.New("pet already has an active booking for this slot")
	ErrCapacityFull = errors.New("slot capacity is full")
	ErrSlotStarted  = errors.New("slot has already started")
	ErrPetNotFound  = errors.New("pet not found")
	ErrForbidden    = errors.New("booking is not owned by this account")
	ErrNotFound     = errors.New("booking not found")
	ErrInvalidState = errors.New("booking state does not allow this operation")
	ErrNotExpired   = errors.New("booking has not expired")
)

type Repository interface {
	CreatePending(context.Context, int64, int64, int64, time.Time) (Booking, error)
	CancelPending(context.Context, int64, int64) (Booking, error)
	CloseExpired(context.Context, int64, time.Time) (Booking, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Book(ctx context.Context, ownerID, petID, slotID int64, now time.Time) (Booking, error) {
	return s.repository.CreatePending(ctx, ownerID, petID, slotID, now)
}

func (s *Service) CancelUnpaid(ctx context.Context, ownerID, bookingID int64) (Booking, error) {
	return s.repository.CancelPending(ctx, ownerID, bookingID)
}

func (s *Service) CloseExpired(ctx context.Context, bookingID int64, now time.Time) (Booking, error) {
	return s.repository.CloseExpired(ctx, bookingID, now)
}
