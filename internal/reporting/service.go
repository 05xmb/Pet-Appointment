package reporting

import (
	"context"
	"errors"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
)

var (
	ErrForbidden   = errors.New("report is not available to this account")
	ErrInvalidDate = errors.New("invalid report date")
)

type Repository interface {
	ListBookings(context.Context, auth.Identity, time.Time, time.Time) ([]BookingView, error)
	DailySummary(context.Context, int64, time.Time, time.Time, string) (DailySummary, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func (s *Service) ListBookings(ctx context.Context, identity auth.Identity, from, to time.Time) ([]BookingView, error) {
	if identity.ID <= 0 || identity.StoreID <= 0 {
		return nil, ErrForbidden
	}
	if identity.Role == "owner" && identity.OwnerID <= 0 {
		return nil, ErrForbidden
	}
	switch identity.Role {
	case "owner", "desk", "worker", "support", "manager":
	default:
		return nil, ErrForbidden
	}
	return s.repository.ListBookings(ctx, identity, from, to)
}

func (s *Service) DailySummary(ctx context.Context, identity auth.Identity, from, to time.Time, date string) (DailySummary, error) {
	if identity.Role != "manager" || identity.StoreID <= 0 {
		return DailySummary{}, ErrForbidden
	}
	return s.repository.DailySummary(ctx, identity.StoreID, from, to, date)
}
