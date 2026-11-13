package payment

import "time"

type Status string

const (
	StatusPending Status = "PENDING"
	StatusSuccess Status = "SUCCESS"
	StatusFailed  Status = "FAILED"
	StatusTimeout Status = "TIMEOUT"
)

const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"
	OutcomeTimeout = "timeout"
)

type Payment struct {
	ID          int64     `json:"id"`
	BookingID   int64     `json:"booking_id"`
	OwnerID     int64     `json:"owner_id"`
	AmountCents int64     `json:"amount_cents"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Callback struct {
	PaymentID   int64  `json:"payment_id"`
	BookingID   int64  `json:"booking_id"`
	Outcome     string `json:"outcome"`
	AmountCents int64  `json:"amount_cents"`
}
