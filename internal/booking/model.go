package booking

import "time"

type Status string

const (
	StatusPendingPayment Status = "PENDING_PAYMENT"
	StatusConfirmed      Status = "CONFIRMED"
	StatusCancelReview   Status = "CANCEL_REVIEW"
	StatusCheckedIn      Status = "CHECKED_IN"
	StatusInService      Status = "IN_SERVICE"
	StatusCompleted      Status = "COMPLETED"
	StatusNoShow         Status = "NO_SHOW"
	StatusClosed         Status = "CLOSED"
	StatusCancelled      Status = "CANCELLED"
)

type Booking struct {
	ID        int64
	SlotID    int64
	PetID     int64
	OwnerID   int64
	Status    Status
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}
