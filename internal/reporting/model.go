package reporting

import (
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
)

type BookingView struct {
	ID                 int64          `json:"id"`
	SlotID             int64          `json:"slot_id"`
	PetID              int64          `json:"pet_id"`
	OwnerID            int64          `json:"owner_id"`
	Status             booking.Status `json:"status"`
	SlotStart          time.Time      `json:"slot_start"`
	SlotEnd            time.Time      `json:"slot_end"`
	PaymentStatus      string         `json:"payment_status,omitempty"`
	CancellationStatus string         `json:"cancellation_status,omitempty"`
	RefundStatus       string         `json:"refund_status,omitempty"`
	ServiceEvents      []string       `json:"service_events,omitempty"`
}

type DailySummary struct {
	Date               string         `json:"date"`
	BookingCount       int            `json:"booking_count"`
	StatusCounts       map[string]int `json:"status_counts"`
	CollectedCents     int64          `json:"collected_cents"`
	RefundedCents      int64          `json:"refunded_cents"`
	PendingRefundCents int64          `json:"pending_refund_cents"`
	OutstandingCents   int64          `json:"outstanding_cents"`
}
