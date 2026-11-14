package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/refund"
)

func (s *Store) CancelConfirmed(ctx context.Context, ownerID, bookingID int64, now time.Time) (booking.Booking, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return booking.Booking{}, err
	}
	defer tx.Rollback()
	b, start, err := lockConfirmedBooking(ctx, tx, ownerID, bookingID)
	if err != nil {
		return booking.Booking{}, err
	}
	if start.Sub(now) < 2*time.Hour {
		return booking.Booking{}, cancellation.ErrTooLate
	}
	paymentID, amount, err := successfulPayment(ctx, tx, bookingID)
	if err != nil {
		return booking.Booking{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CANCELLED' WHERE id = ? AND status = 'CONFIRMED'`, bookingID); err != nil {
		return booking.Booking{}, err
	}
	if err := insertRefund(ctx, tx, bookingID, paymentID, amount); err != nil {
		return booking.Booking{}, err
	}
	b.Status = booking.StatusCancelled
	if err := writeAudit(ctx, tx, ownerID, "booking", bookingID, "cancelled_early", string(booking.StatusConfirmed), string(booking.StatusCancelled), "full refund pending"); err != nil {
		return booking.Booking{}, err
	}
	if err := tx.Commit(); err != nil {
		return booking.Booking{}, err
	}
	return b, nil
}

func (s *Store) RequestCancellation(ctx context.Context, ownerID, bookingID int64, reason string, now time.Time) (cancellation.Request, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return cancellation.Request{}, cancellation.ErrInvalidState
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cancellation.Request{}, err
	}
	defer tx.Rollback()
	if err := writeAudit(ctx, tx, ownerID, "booking", bookingID, "cancellation_requested", string(booking.StatusConfirmed), string(booking.StatusCancelReview), reason); err != nil {
		return cancellation.Request{}, err
	}
	var status string
	var bookingOwner int64
	var start time.Time
	err = tx.QueryRowContext(ctx, `SELECT b.status, b.owner_id, s.start_at FROM bookings b JOIN slots s ON s.id = b.slot_id
		WHERE b.id = ? FOR UPDATE`, bookingID).Scan(&status, &bookingOwner, &start)
	if errors.Is(err, sql.ErrNoRows) {
		return cancellation.Request{}, booking.ErrNotFound
	}
	if err != nil {
		return cancellation.Request{}, err
	}
	if bookingOwner != ownerID {
		return cancellation.Request{}, cancellation.ErrForbidden
	}
	if status != string(booking.StatusConfirmed) {
		return cancellation.Request{}, cancellation.ErrInvalidState
	}
	if start.Sub(now) >= 2*time.Hour {
		return cancellation.Request{}, cancellation.ErrTooEarly
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CANCEL_REVIEW' WHERE id = ? AND status = 'CONFIRMED'`, bookingID); err != nil {
		return cancellation.Request{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO cancellation_requests (booking_id, owner_id, reason, status)
		VALUES (?, ?, ?, 'PENDING')`, bookingID, ownerID, reason)
	if err != nil {
		return cancellation.Request{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return cancellation.Request{}, err
	}
	var request cancellation.Request
	err = tx.QueryRowContext(ctx, `SELECT id, booking_id, owner_id, reason, status, reviewed_by, created_at
		FROM cancellation_requests WHERE id = ?`, id).Scan(&request.ID, &request.BookingID, &request.OwnerID, &request.Reason, &request.Status, &request.ReviewedBy, &request.CreatedAt)
	if err != nil {
		return cancellation.Request{}, err
	}
	if err := tx.Commit(); err != nil {
		return cancellation.Request{}, err
	}
	return request, nil
}

func (s *Store) ReviewCancellation(ctx context.Context, supportID, requestID int64, approve bool, now time.Time) (booking.Booking, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return booking.Booking{}, err
	}
	defer tx.Rollback()
	var role string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM accounts WHERE id = ? FOR UPDATE`, supportID).Scan(&role); errors.Is(err, sql.ErrNoRows) {
		return booking.Booking{}, cancellation.ErrForbidden
	} else if err != nil {
		return booking.Booking{}, err
	}
	if role != "support" {
		return booking.Booking{}, cancellation.ErrForbidden
	}
	var bookingID int64
	var requestStatus string
	if err := tx.QueryRowContext(ctx, `SELECT booking_id, status FROM cancellation_requests WHERE id = ? FOR UPDATE`, requestID).Scan(&bookingID, &requestStatus); errors.Is(err, sql.ErrNoRows) {
		return booking.Booking{}, booking.ErrNotFound
	} else if err != nil {
		return booking.Booking{}, err
	}
	b, err := lockBooking(ctx, tx, bookingID)
	if err != nil {
		return booking.Booking{}, err
	}
	if requestStatus != string(cancellation.StatusPending) || b.Status != booking.StatusCancelReview {
		return booking.Booking{}, cancellation.ErrInvalidState
	}
	if approve {
		paymentID, amount, err := successfulPayment(ctx, tx, bookingID)
		if err != nil {
			return booking.Booking{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CANCELLED' WHERE id = ? AND status = 'CANCEL_REVIEW'`, bookingID); err != nil {
			return booking.Booking{}, err
		}
		if err := insertRefund(ctx, tx, bookingID, paymentID, amount); err != nil {
			return booking.Booking{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE cancellation_requests SET status = 'APPROVED', reviewed_by = ?, reviewed_at = ? WHERE id = ?`, supportID, now.UTC(), requestID); err != nil {
			return booking.Booking{}, err
		}
		b.Status = booking.StatusCancelled
		if err := writeAudit(ctx, tx, supportID, "booking", bookingID, "cancellation_approved", string(booking.StatusCancelReview), string(booking.StatusCancelled), "full refund pending"); err != nil {
			return booking.Booking{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CONFIRMED' WHERE id = ? AND status = 'CANCEL_REVIEW'`, bookingID); err != nil {
			return booking.Booking{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE cancellation_requests SET status = 'REJECTED', reviewed_by = ?, reviewed_at = ? WHERE id = ?`, supportID, now.UTC(), requestID); err != nil {
			return booking.Booking{}, err
		}
		b.Status = booking.StatusConfirmed
		if err := writeAudit(ctx, tx, supportID, "booking", bookingID, "cancellation_rejected", string(booking.StatusCancelReview), string(booking.StatusConfirmed), ""); err != nil {
			return booking.Booking{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return booking.Booking{}, err
	}
	return b, nil
}

func (s *Store) ApplyRefundResult(ctx context.Context, refundID int64, success bool) (refund.Refund, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return refund.Refund{}, err
	}
	defer tx.Rollback()
	var result refund.Refund
	err = tx.QueryRowContext(ctx, `SELECT id, booking_id, payment_id, amount_cents, status, created_at, updated_at
		FROM refunds WHERE id = ? FOR UPDATE`, refundID).Scan(&result.ID, &result.BookingID, &result.PaymentID, &result.AmountCents, &result.Status, &result.CreatedAt, &result.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return refund.Refund{}, cancellation.ErrInvalidState
	}
	if err != nil {
		return refund.Refund{}, err
	}
	if result.Status == refund.StatusSuccess {
		return result, nil
	}
	if result.Status != refund.StatusPending && result.Status != refund.StatusFailed {
		return result, nil
	}
	newStatus := refund.StatusFailed
	if success {
		newStatus = refund.StatusSuccess
	}
	oldStatus := result.Status
	if _, err := tx.ExecContext(ctx, `UPDATE refunds SET status = ? WHERE id = ? AND status IN ('PENDING', 'FAILED')`, newStatus, refundID); err != nil {
		return refund.Refund{}, err
	}
	result.Status = newStatus
	if err := writeAudit(ctx, tx, 0, "refund", refundID, "callback", string(oldStatus), string(newStatus), ""); err != nil {
		return refund.Refund{}, err
	}
	if err := tx.Commit(); err != nil {
		return refund.Refund{}, err
	}
	return result, nil
}

func lockConfirmedBooking(ctx context.Context, tx *sql.Tx, ownerID, bookingID int64) (booking.Booking, time.Time, error) {
	b, err := lockBooking(ctx, tx, bookingID)
	if err != nil {
		return booking.Booking{}, time.Time{}, err
	}
	if b.OwnerID != ownerID {
		return booking.Booking{}, time.Time{}, cancellation.ErrForbidden
	}
	if b.Status != booking.StatusConfirmed {
		return booking.Booking{}, time.Time{}, cancellation.ErrInvalidState
	}
	var start time.Time
	if err := tx.QueryRowContext(ctx, `SELECT start_at FROM slots WHERE id = ?`, b.SlotID).Scan(&start); err != nil {
		return booking.Booking{}, time.Time{}, err
	}
	return b, start, nil
}

func successfulPayment(ctx context.Context, tx *sql.Tx, bookingID int64) (int64, int64, error) {
	var id, amount int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT id, amount_cents, status FROM payments WHERE booking_id = ? FOR UPDATE`, bookingID).Scan(&id, &amount, &status); errors.Is(err, sql.ErrNoRows) {
		return 0, 0, payment.ErrBookingNotPayable
	} else if err != nil {
		return 0, 0, err
	}
	if status != string(payment.StatusSuccess) {
		return 0, 0, payment.ErrBookingNotPayable
	}
	return id, amount, nil
}

func insertRefund(ctx context.Context, tx *sql.Tx, bookingID, paymentID, amount int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO refunds (booking_id, payment_id, amount_cents, status)
		VALUES (?, ?, ?, 'PENDING') ON DUPLICATE KEY UPDATE booking_id = booking_id`, bookingID, paymentID, amount)
	return err
}
