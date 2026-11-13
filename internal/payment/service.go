package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidSignature      = errors.New("invalid payment signature")
	ErrInvalidCallback       = errors.New("invalid payment callback")
	ErrBookingNotPayable     = errors.New("booking is not payable")
	ErrPaymentNotFound       = errors.New("payment not found")
	ErrPaymentAmountMismatch = errors.New("payment amount mismatch")
)

type Repository interface {
	CreatePayment(context.Context, int64, int64, time.Time) (Payment, error)
	ApplyCallback(context.Context, Callback, time.Time) (Payment, error)
}

type Service struct {
	repository Repository
	secret     []byte
}

func NewService(repository Repository, secret []byte) *Service {
	return &Service{repository: repository, secret: append([]byte(nil), secret...)}
}

func Sign(secret, raw []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func Verify(secret, raw []byte, signature string) bool {
	provided, err := hex.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	expected, err := hex.DecodeString(Sign(secret, raw))
	if err != nil || len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare(expected, provided) == 1
}

func (s *Service) CreatePayment(ctx context.Context, ownerID, bookingID int64, now time.Time) (Payment, error) {
	return s.repository.CreatePayment(ctx, ownerID, bookingID, now)
}

func (s *Service) Callback(ctx context.Context, raw []byte, signature string, now time.Time) (Payment, error) {
	if len(s.secret) == 0 || !Verify(s.secret, raw, signature) {
		return Payment{}, ErrInvalidSignature
	}
	var callback Callback
	if err := json.Unmarshal(raw, &callback); err != nil || callback.PaymentID <= 0 || callback.BookingID <= 0 || callback.AmountCents <= 0 {
		return Payment{}, ErrInvalidCallback
	}
	switch callback.Outcome {
	case OutcomeSuccess, OutcomeFailed, OutcomeTimeout:
	default:
		return Payment{}, ErrInvalidCallback
	}
	return s.repository.ApplyCallback(ctx, callback, now)
}
