package fulfillment

import (
	"context"
	"errors"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
)

var (
	ErrForbidden            = errors.New("fulfillment operation is not allowed")
	ErrNotFound             = errors.New("fulfillment booking not found")
	ErrInvalidState         = errors.New("booking state does not allow this fulfillment operation")
	ErrOutsideCheckInWindow = errors.New("booking is outside the check-in window")
	ErrTooEarly             = errors.New("booking cannot be marked no-show before the slot ends")
	ErrReasonRequired       = errors.New("no-show reason is required")
)

type Repository interface {
	CheckIn(context.Context, int64, int64, time.Time) (booking.Booking, Event, error)
	StartService(context.Context, int64, int64, time.Time) (booking.Booking, Event, error)
	CompleteService(context.Context, int64, int64, time.Time) (booking.Booking, Event, error)
	MarkNoShow(context.Context, int64, int64, string, time.Time) (booking.Booking, Event, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) CheckIn(ctx context.Context, staffID, bookingID int64, now time.Time) (booking.Booking, Event, error) {
	return s.repository.CheckIn(ctx, staffID, bookingID, now)
}

func (s *Service) Start(ctx context.Context, staffID, bookingID int64, now time.Time) (booking.Booking, Event, error) {
	return s.repository.StartService(ctx, staffID, bookingID, now)
}

func (s *Service) Complete(ctx context.Context, staffID, bookingID int64, now time.Time) (booking.Booking, Event, error) {
	return s.repository.CompleteService(ctx, staffID, bookingID, now)
}

func (s *Service) NoShow(ctx context.Context, managerID, bookingID int64, reason string, now time.Time) (booking.Booking, Event, error) {
	return s.repository.MarkNoShow(ctx, managerID, bookingID, reason, now)
}
