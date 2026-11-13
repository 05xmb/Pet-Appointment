package slot

import "time"

type Slot struct {
	ID        int64     `json:"id"`
	StoreID   int64     `json:"store_id"`
	ProjectID int64     `json:"project_id"`
	StartAt   time.Time `json:"start_at"`
	EndAt     time.Time `json:"end_at"`
	Capacity  int       `json:"capacity"`
}
