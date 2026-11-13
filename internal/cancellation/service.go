package cancellation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/refund"
)

var (
	ErrInvalidState     = errors.New("cancellation state does not allow this operation")
	ErrForbidden        = errors.New("cancellation operation is not allowed")
	ErrTooLate          = errors.New("direct cancellation window has closed")
	ErrTooEarly         = errors.New("cancellation request is only for the late window")
	ErrInvalidSignature = errors.New("invalid refund signature")
	ErrInvalidCallback  = errors.New("invalid refund callback")
)

type Repository interface {
	CancelConfirmed(context.Context, int64, int64, time.Time) (booking.Booking, error)
	RequestCancellation(context.Context, int64, int64, string, time.Time) (Request, error)
	ReviewCancellation(context.Context, int64, int64, bool, time.Time) (booking.Booking, error)
	ApplyRefundResult(context.Context, int64, bool) (refund.Refund, error)
}

type Service struct {
	repository Repository
	secret     []byte
}

func NewService(repository Repository) *Service { return &Service{repository: repository} }

func NewServiceWithSecret(repository Repository, secret []byte) *Service {
	return &Service{repository: repository, secret: append([]byte(nil), secret...)}
}

func (s *Service) CancelConfirmed(ctx context.Context, ownerID, bookingID int64, now time.Time) (booking.Booking, error) {
	return s.repository.CancelConfirmed(ctx, ownerID, bookingID, now)
}

func (s *Service) Request(ctx context.Context, ownerID, bookingID int64, reason string, now time.Time) (Request, error) {
	return s.repository.RequestCancellation(ctx, ownerID, bookingID, reason, now)
}

func (s *Service) Review(ctx context.Context, supportID, requestID int64, approve bool, now time.Time) (booking.Booking, error) {
	return s.repository.ReviewCancellation(ctx, supportID, requestID, approve, now)
}

func (s *Service) RefundResult(ctx context.Context, refundID int64, success bool) (refund.Refund, error) {
	return s.repository.ApplyRefundResult(ctx, refundID, success)
}

func (s *Service) RefundCallback(ctx context.Context, raw []byte, signature string) (refund.Refund, error) {
	if len(s.secret) == 0 || !payment.Verify(s.secret, raw, signature) {
		return refund.Refund{}, ErrInvalidSignature
	}
	var callback struct {
		RefundID int64  `json:"refund_id"`
		Outcome  string `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &callback); err != nil || callback.RefundID <= 0 {
		return refund.Refund{}, ErrInvalidCallback
	}
	var success bool
	switch callback.Outcome {
	case "success":
		success = true
	case "failed":
	default:
		return refund.Refund{}, ErrInvalidCallback
	}
	return s.repository.ApplyRefundResult(ctx, callback.RefundID, success)
}
