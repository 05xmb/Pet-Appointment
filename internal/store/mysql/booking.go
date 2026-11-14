package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
)

func (s *Store) CreatePending(ctx context.Context, ownerID, petID, slotID int64, now time.Time) (booking.Booking, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return booking.Booking{}, err
	}
	defer tx.Rollback()

	var slotStart time.Time
	var storeID int64
	if err := tx.QueryRowContext(ctx, `SELECT store_id, start_at FROM slots WHERE id = ? FOR UPDATE`, slotID).Scan(&storeID, &slotStart); errors.Is(err, sql.ErrNoRows) {
		return booking.Booking{}, booking.ErrNotFound
	} else if err != nil {
		return booking.Booking{}, err
	}
	if !now.Before(slotStart) {
		return booking.Booking{}, booking.ErrSlotStarted
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM bookings
		WHERE slot_id = ? AND status = 'PENDING_PAYMENT' AND expires_at <= ? FOR UPDATE`, slotID, now.UTC())
	if err != nil {
		return booking.Booking{}, err
	}
	var expiredIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return booking.Booking{}, err
		}
		expiredIDs = append(expiredIDs, id)
	}
	if err := rows.Close(); err != nil {
		return booking.Booking{}, err
	}
	if err := rows.Err(); err != nil {
		return booking.Booking{}, err
	}
	for _, id := range expiredIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CLOSED' WHERE id = ? AND status = 'PENDING_PAYMENT'`, id); err != nil {
			return booking.Booking{}, err
		}
		if err := writeAudit(ctx, tx, 0, "booking", id, "expired", string(booking.StatusPendingPayment), string(booking.StatusClosed), ""); err != nil {
			return booking.Booking{}, err
		}
	}

	var petOwnerID int64
	if err := tx.QueryRowContext(ctx, `SELECT owner_id FROM pets WHERE id = ? AND store_id = ? FOR UPDATE`, petID, storeID).Scan(&petOwnerID); errors.Is(err, sql.ErrNoRows) {
		return booking.Booking{}, booking.ErrPetNotFound
	} else if err != nil {
		return booking.Booking{}, err
	}
	if petOwnerID != ownerID {
		return booking.Booking{}, booking.ErrForbidden
	}

	var duplicate int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bookings
		WHERE slot_id = ? AND pet_id = ? AND status NOT IN ('CLOSED', 'CANCELLED', 'NO_SHOW')`, slotID, petID).Scan(&duplicate); err != nil {
		return booking.Booking{}, err
	}
	if duplicate > 0 {
		return booking.Booking{}, booking.ErrDuplicatePet
	}
	var occupied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bookings
		WHERE slot_id = ? AND status IN ('PENDING_PAYMENT', 'CONFIRMED', 'CANCEL_REVIEW', 'CHECKED_IN', 'IN_SERVICE', 'COMPLETED')`, slotID).Scan(&occupied); err != nil {
		return booking.Booking{}, err
	}
	var capacity int
	if err := tx.QueryRowContext(ctx, `SELECT capacity FROM slots WHERE id = ?`, slotID).Scan(&capacity); err != nil {
		return booking.Booking{}, err
	}
	if occupied >= capacity {
		return booking.Booking{}, booking.ErrCapacityFull
	}

	expires := now.UTC().Add(5 * time.Minute)
	result, err := tx.ExecContext(ctx, `INSERT INTO bookings (slot_id, pet_id, owner_id, status, expires_at)
		VALUES (?, ?, ?, 'PENDING_PAYMENT', ?)`, slotID, petID, ownerID, expires)
	if err != nil {
		return booking.Booking{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return booking.Booking{}, err
	}
	created := booking.Booking{ID: id, SlotID: slotID, PetID: petID, OwnerID: ownerID, Status: booking.StatusPendingPayment, ExpiresAt: expires}
	if err := tx.QueryRowContext(ctx, `SELECT created_at, updated_at FROM bookings WHERE id = ?`, id).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
		return booking.Booking{}, err
	}
	if err := writeAudit(ctx, tx, ownerID, "booking", id, "created", "", string(booking.StatusPendingPayment), ""); err != nil {
		return booking.Booking{}, err
	}
	if err := tx.Commit(); err != nil {
		return booking.Booking{}, err
	}
	return created, nil
}

func (s *Store) CloseExpired(ctx context.Context, bookingID int64, now time.Time) (booking.Booking, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return booking.Booking{}, err
	}
	defer tx.Rollback()
	b, err := lockBooking(ctx, tx, bookingID)
	if err != nil {
		return booking.Booking{}, err
	}
	if b.Status == booking.StatusClosed {
		return b, nil
	}
	if b.Status != booking.StatusPendingPayment {
		return booking.Booking{}, booking.ErrInvalidState
	}
	if now.Before(b.ExpiresAt) {
		return booking.Booking{}, booking.ErrNotExpired
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CLOSED' WHERE id = ? AND status = 'PENDING_PAYMENT'`, bookingID); err != nil {
		return booking.Booking{}, err
	}
	b.Status = booking.StatusClosed
	if err := writeAudit(ctx, tx, 0, "booking", bookingID, "expired", string(booking.StatusPendingPayment), string(booking.StatusClosed), ""); err != nil {
		return booking.Booking{}, err
	}
	if err := tx.Commit(); err != nil {
		return booking.Booking{}, err
	}
	return b, nil
}

func (s *Store) CancelPending(ctx context.Context, ownerID, bookingID int64) (booking.Booking, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return booking.Booking{}, err
	}
	defer tx.Rollback()
	b, err := lockBooking(ctx, tx, bookingID)
	if err != nil {
		return booking.Booking{}, err
	}
	if b.OwnerID != ownerID {
		return booking.Booking{}, booking.ErrForbidden
	}
	if b.Status == booking.StatusCancelled {
		return b, nil
	}
	if b.Status != booking.StatusPendingPayment {
		return booking.Booking{}, booking.ErrInvalidState
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CANCELLED' WHERE id = ? AND status = 'PENDING_PAYMENT'`, bookingID); err != nil {
		return booking.Booking{}, err
	}
	b.Status = booking.StatusCancelled
	if err := writeAudit(ctx, tx, ownerID, "booking", bookingID, "cancelled_unpaid", string(booking.StatusPendingPayment), string(booking.StatusCancelled), ""); err != nil {
		return booking.Booking{}, err
	}
	if err := tx.Commit(); err != nil {
		return booking.Booking{}, err
	}
	return b, nil
}

func lockBooking(ctx context.Context, tx *sql.Tx, id int64) (booking.Booking, error) {
	var b booking.Booking
	var status string
	err := tx.QueryRowContext(ctx, `SELECT id, slot_id, pet_id, owner_id, status, expires_at, created_at, updated_at
		FROM bookings WHERE id = ? FOR UPDATE`, id).Scan(
		&b.ID, &b.SlotID, &b.PetID, &b.OwnerID, &status, &b.ExpiresAt, &b.CreatedAt, &b.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return booking.Booking{}, booking.ErrNotFound
	}
	if err != nil {
		return booking.Booking{}, err
	}
	b.Status = booking.Status(status)
	return b, nil
}
