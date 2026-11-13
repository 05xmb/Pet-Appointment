package fulfillment

import "time"

type EventType string

const (
	EventCheckIn      EventType = "CHECK_IN"
	EventServiceStart EventType = "SERVICE_START"
	EventServiceDone  EventType = "SERVICE_COMPLETE"
	EventNoShow       EventType = "NO_SHOW"
)

type Event struct {
	ID         int64     `json:"id"`
	BookingID  int64     `json:"booking_id"`
	ActorID    int64     `json:"actor_id"`
	Type       EventType `json:"type"`
	Reason     string    `json:"reason,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}
