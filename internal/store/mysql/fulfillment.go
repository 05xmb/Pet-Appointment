package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
)

func (s *Store) CheckIn(ctx context.Context, staffID, bookingID int64, now time.Time) (booking.Booking, fulfillment.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	defer tx.Rollback()
	actorStore, err := requireRole(ctx, tx, staffID, "desk")
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	b, start, end, slotStore, err := lockBookingSlot(ctx, tx, bookingID)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	if actorStore != slotStore {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrForbidden
	}
	if now.Before(start.Add(-30*time.Minute)) || now.After(end) {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrOutsideCheckInWindow
	}
	if b.Status != booking.StatusConfirmed {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrInvalidState
	}
	updated, event, err := recordTransition(ctx, tx, b, staffID, fulfillment.EventCheckIn, "", booking.StatusCheckedIn, now)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	return updated, event, nil
}

func (s *Store) StartService(ctx context.Context, staffID, bookingID int64, now time.Time) (booking.Booking, fulfillment.Event, error) {
	return s.transitionService(ctx, staffID, bookingID, now, fulfillment.EventServiceStart, booking.StatusCheckedIn, booking.StatusInService)
}

func (s *Store) CompleteService(ctx context.Context, staffID, bookingID int64, now time.Time) (booking.Booking, fulfillment.Event, error) {
	return s.transitionService(ctx, staffID, bookingID, now, fulfillment.EventServiceDone, booking.StatusInService, booking.StatusCompleted)
}

func (s *Store) transitionService(ctx context.Context, staffID, bookingID int64, now time.Time, eventType fulfillment.EventType, from, to booking.Status) (booking.Booking, fulfillment.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	defer tx.Rollback()
	actorStore, err := requireRole(ctx, tx, staffID, "worker")
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	b, _, end, slotStore, err := lockBookingSlot(ctx, tx, bookingID)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	if actorStore != slotStore {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrForbidden
	}
	if eventType == fulfillment.EventServiceStart && now.After(end) {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrInvalidState
	}
	if b.Status != from {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrInvalidState
	}
	updated, event, err := recordTransition(ctx, tx, b, staffID, eventType, "", to, now)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	return updated, event, nil
}

func (s *Store) MarkNoShow(ctx context.Context, managerID, bookingID int64, reason string, now time.Time) (booking.Booking, fulfillment.Event, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrReasonRequired
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	defer tx.Rollback()
	actorStore, err := requireRole(ctx, tx, managerID, "manager")
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	b, _, end, slotStore, err := lockBookingSlot(ctx, tx, bookingID)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	if actorStore != slotStore {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrForbidden
	}
	if now.Before(end) {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrTooEarly
	}
	if b.Status != booking.StatusConfirmed {
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrInvalidState
	}
	updated, event, err := recordTransition(ctx, tx, b, managerID, fulfillment.EventNoShow, reason, booking.StatusNoShow, now)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	return updated, event, nil
}

func requireRole(ctx context.Context, tx *sql.Tx, accountID int64, want string) (int64, error) {
	var role string
	var storeID int64
	if err := tx.QueryRowContext(ctx, `SELECT role, store_id FROM accounts WHERE id = ? FOR UPDATE`, accountID).Scan(&role, &storeID); errors.Is(err, sql.ErrNoRows) {
		return 0, fulfillment.ErrForbidden
	} else if err != nil {
		return 0, err
	}
	if role != want {
		return 0, fulfillment.ErrForbidden
	}
	return storeID, nil
}

func lockBookingSlot(ctx context.Context, tx *sql.Tx, bookingID int64) (booking.Booking, time.Time, time.Time, int64, error) {
	b, err := lockBooking(ctx, tx, bookingID)
	if err != nil {
		if errors.Is(err, booking.ErrNotFound) {
			return booking.Booking{}, time.Time{}, time.Time{}, 0, fulfillment.ErrNotFound
		}
		return booking.Booking{}, time.Time{}, time.Time{}, 0, err
	}
	var start, end time.Time
	var storeID int64
	if err := tx.QueryRowContext(ctx, `SELECT store_id, start_at, end_at FROM slots WHERE id = ? FOR UPDATE`, b.SlotID).Scan(&storeID, &start, &end); err != nil {
		return booking.Booking{}, time.Time{}, time.Time{}, 0, err
	}
	return b, start, end, storeID, nil
}

func recordTransition(ctx context.Context, tx *sql.Tx, b booking.Booking, actorID int64, eventType fulfillment.EventType, reason string, next booking.Status, now time.Time) (booking.Booking, fulfillment.Event, error) {
	result, err := tx.ExecContext(ctx, `UPDATE bookings SET status = ? WHERE id = ? AND status = ?`, next, b.ID, b.Status)
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		if err != nil {
			return booking.Booking{}, fulfillment.Event{}, err
		}
		return booking.Booking{}, fulfillment.Event{}, fulfillment.ErrInvalidState
	}
	insert, err := tx.ExecContext(ctx, `INSERT INTO service_events (booking_id, actor_id, type, reason, occurred_at)
		VALUES (?, ?, ?, ?, ?)`, b.ID, actorID, eventType, reason, now.UTC())
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	if err := writeAudit(ctx, tx, actorID, "booking", b.ID, string(eventType), string(b.Status), string(next), reason); err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	eventID, err := insert.LastInsertId()
	if err != nil {
		return booking.Booking{}, fulfillment.Event{}, err
	}
	b.Status = next
	b.UpdatedAt = now.UTC()
	return b, fulfillment.Event{ID: eventID, BookingID: b.ID, ActorID: actorID, Type: eventType, Reason: reason, OccurredAt: now.UTC()}, nil
}
