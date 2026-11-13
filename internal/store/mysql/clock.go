package mysqlstore

import (
	"context"
	"fmt"
	"time"
)

const maxClockOffsetSeconds = int64((1<<63 - 1) / int64(time.Second))

func (s *Store) ClockNow(ctx context.Context, realNow time.Time) (time.Time, error) {
	var offsetSeconds int64
	if err := s.db.QueryRowContext(ctx, `SELECT offset_seconds FROM app_clock WHERE id = 1`).Scan(&offsetSeconds); err != nil {
		return time.Time{}, err
	}
	if offsetSeconds < 0 || offsetSeconds > maxClockOffsetSeconds {
		return time.Time{}, fmt.Errorf("clock offset outside supported duration")
	}
	return realNow.Add(time.Duration(offsetSeconds) * time.Second), nil
}

func (s *Store) AdvanceClock(ctx context.Context, minutes int64) error {
	if minutes <= 0 || minutes > 365*24*60 {
		return fmt.Errorf("clock advance must be between 1 minute and 1 year")
	}
	seconds := minutes * 60
	result, err := s.db.ExecContext(ctx, `UPDATE app_clock SET offset_seconds = offset_seconds + ?
		WHERE id = 1 AND offset_seconds BETWEEN 0 AND ?`, seconds, maxClockOffsetSeconds-seconds)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("clock advance exceeds supported duration")
	}
	return nil
}
