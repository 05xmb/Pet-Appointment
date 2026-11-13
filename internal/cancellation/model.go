package cancellation

import "time"

type RequestStatus string

const (
	StatusPending  RequestStatus = "PENDING"
	StatusApproved RequestStatus = "APPROVED"
	StatusRejected RequestStatus = "REJECTED"
)

type Request struct {
	ID         int64         `json:"id"`
	BookingID  int64         `json:"booking_id"`
	OwnerID    int64         `json:"owner_id"`
	Reason     string        `json:"reason"`
	Status     RequestStatus `json:"status"`
	ReviewedBy int64         `json:"reviewed_by"`
	CreatedAt  time.Time     `json:"created_at"`
	ReviewedAt *time.Time    `json:"reviewed_at,omitempty"`
}
