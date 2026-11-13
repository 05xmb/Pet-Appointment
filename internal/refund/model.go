package refund

import "time"

type Status string

const (
	StatusPending Status = "PENDING"
	StatusSuccess Status = "SUCCESS"
	StatusFailed  Status = "FAILED"
)

type Refund struct {
	ID          int64     `json:"id"`
	BookingID   int64     `json:"booking_id"`
	PaymentID   int64     `json:"payment_id"`
	AmountCents int64     `json:"amount_cents"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
