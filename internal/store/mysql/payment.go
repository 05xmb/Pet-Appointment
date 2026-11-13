package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/payment"
)

func (s *Store) CreatePayment(ctx context.Context, ownerID, bookingID int64, now time.Time) (payment.Payment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return payment.Payment{}, err
	}
	defer tx.Rollback()
	var existing payment.Payment
	err = tx.QueryRowContext(ctx, `SELECT id, booking_id, owner_id, amount_cents, status, created_at, updated_at
		FROM payments WHERE booking_id = ? FOR UPDATE`, bookingID).Scan(
		&existing.ID, &existing.BookingID, &existing.OwnerID, &existing.AmountCents, &existing.Status, &existing.CreatedAt, &existing.UpdatedAt,
	)
	if err == nil {
		if existing.OwnerID != ownerID {
			return payment.Payment{}, booking.ErrForbidden
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return payment.Payment{}, err
	}
	var status string
	var bookingOwner, amount int64
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT b.status, b.owner_id, b.expires_at, p.price_cents FROM bookings b
		JOIN slots s ON s.id = b.slot_id JOIN projects p ON p.id = s.project_id
		WHERE b.id = ? FOR UPDATE`, bookingID).Scan(&status, &bookingOwner, &expiresAt, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return payment.Payment{}, booking.ErrNotFound
	}
	if err != nil {
		return payment.Payment{}, err
	}
	if bookingOwner != ownerID {
		return payment.Payment{}, booking.ErrForbidden
	}
	if status != string(booking.StatusPendingPayment) {
		return payment.Payment{}, payment.ErrBookingNotPayable
	}
	if !now.Before(expiresAt) {
		if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CLOSED' WHERE id = ? AND status = 'PENDING_PAYMENT'`, bookingID); err != nil {
			return payment.Payment{}, err
		}
		if err := tx.Commit(); err != nil {
			return payment.Payment{}, err
		}
		return payment.Payment{}, payment.ErrBookingNotPayable
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO payments (booking_id, owner_id, amount_cents, status)
		VALUES (?, ?, ?, 'PENDING')`, bookingID, ownerID, amount)
	if err != nil {
		return payment.Payment{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return payment.Payment{}, err
	}
	created := payment.Payment{ID: id, BookingID: bookingID, OwnerID: ownerID, AmountCents: amount, Status: payment.StatusPending}
	if err := tx.QueryRowContext(ctx, `SELECT created_at, updated_at FROM payments WHERE id = ?`, id).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
		return payment.Payment{}, err
	}
	if err := tx.Commit(); err != nil {
		return payment.Payment{}, err
	}
	return created, nil
}

func (s *Store) ApplyCallback(ctx context.Context, callback payment.Callback, now time.Time) (payment.Payment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return payment.Payment{}, err
	}
	defer tx.Rollback()
	var result payment.Payment
	var bookingStatus string
	err = tx.QueryRowContext(ctx, `SELECT p.id, p.booking_id, p.owner_id, p.amount_cents, p.status,
		p.created_at, p.updated_at, b.status FROM payments p JOIN bookings b ON b.id = p.booking_id
		WHERE p.id = ? FOR UPDATE`, callback.PaymentID).Scan(
		&result.ID, &result.BookingID, &result.OwnerID, &result.AmountCents, &result.Status,
		&result.CreatedAt, &result.UpdatedAt, &bookingStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return payment.Payment{}, payment.ErrPaymentNotFound
	}
	if err != nil {
		return payment.Payment{}, err
	}
	if result.BookingID != callback.BookingID {
		return payment.Payment{}, payment.ErrInvalidCallback
	}
	if result.AmountCents != callback.AmountCents {
		return payment.Payment{}, payment.ErrPaymentAmountMismatch
	}
	if result.Status != payment.StatusPending {
		return result, nil
	}
	newStatus := payment.StatusFailed
	if callback.Outcome == payment.OutcomeSuccess {
		newStatus = payment.StatusSuccess
	} else if callback.Outcome == payment.OutcomeTimeout {
		newStatus = payment.StatusTimeout
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET status = ? WHERE id = ? AND status = 'PENDING'`, newStatus, callback.PaymentID); err != nil {
		return payment.Payment{}, err
	}
	result.Status = newStatus
	if callback.Outcome == payment.OutcomeSuccess && bookingStatus == string(booking.StatusPendingPayment) {
		if _, err := tx.ExecContext(ctx, `UPDATE bookings SET status = 'CONFIRMED' WHERE id = ? AND status = 'PENDING_PAYMENT'`, callback.BookingID); err != nil {
			return payment.Payment{}, err
		}
	} else if callback.Outcome == payment.OutcomeSuccess {
		if _, err := tx.ExecContext(ctx, `INSERT INTO exceptions (payment_id, booking_id, type, status, amount_cents, detail)
			VALUES (?, ?, 'LATE_PAYMENT', 'NEEDS_REFUND', ?, '支付成功时预约已关闭或取消')
			ON DUPLICATE KEY UPDATE payment_id = payment_id`, callback.PaymentID, callback.BookingID, callback.AmountCents); err != nil {
			return payment.Payment{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return payment.Payment{}, err
	}
	_ = now
	return result, nil
}
