package mysqlstore

import (
	"context"
	"strings"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/reporting"
)

func (s *Store) ListBookings(ctx context.Context, identity auth.Identity, from, to time.Time) ([]reporting.BookingView, error) {
	query := `SELECT b.id, b.slot_id, b.pet_id, b.owner_id, b.status, s.start_at, s.end_at,
		COALESCE(p.status, ''), COALESCE(cr.status, ''), COALESCE(r.status, ''),
		COALESCE(GROUP_CONCAT(se.type ORDER BY se.occurred_at SEPARATOR ','), '')
		FROM bookings b
		JOIN slots s ON s.id = b.slot_id
		LEFT JOIN payments p ON p.booking_id = b.id
		LEFT JOIN cancellation_requests cr ON cr.booking_id = b.id
		LEFT JOIN refunds r ON r.booking_id = b.id
		LEFT JOIN service_events se ON se.booking_id = b.id
		WHERE s.store_id = ? AND s.start_at >= ? AND s.start_at < ?`
	args := []any{identity.StoreID, from.UTC(), to.UTC()}
	if identity.Role == "owner" {
		query += ` AND b.owner_id = ?`
		args = append(args, identity.OwnerID)
	}
	query += ` GROUP BY b.id, b.slot_id, b.pet_id, b.owner_id, b.status, s.start_at, s.end_at, p.status, cr.status, r.status ORDER BY s.start_at, b.id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []reporting.BookingView
	for rows.Next() {
		var view reporting.BookingView
		var status, paymentStatus, cancellationStatus, refundStatus, eventTypes string
		if err := rows.Scan(&view.ID, &view.SlotID, &view.PetID, &view.OwnerID, &status, &view.SlotStart, &view.SlotEnd,
			&paymentStatus, &cancellationStatus, &refundStatus, &eventTypes); err != nil {
			return nil, err
		}
		view.Status = bookingStatus(status)
		view.PaymentStatus = paymentStatus
		view.CancellationStatus = cancellationStatus
		view.RefundStatus = refundStatus
		if eventTypes != "" {
			view.ServiceEvents = strings.Split(eventTypes, ",")
		}
		result = append(result, view)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) DailySummary(ctx context.Context, storeID int64, from, to time.Time, date string) (reporting.DailySummary, error) {
	result := reporting.DailySummary{Date: date, StatusCounts: make(map[string]int)}
	rows, err := s.db.QueryContext(ctx, `SELECT b.status, COUNT(*),
		COALESCE(SUM(CASE WHEN p.status = 'SUCCESS' THEN p.amount_cents ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN r.status = 'SUCCESS' THEN r.amount_cents ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN r.status IN ('PENDING', 'FAILED') THEN r.amount_cents ELSE 0 END), 0)
		FROM bookings b
		JOIN slots s ON s.id = b.slot_id
		LEFT JOIN payments p ON p.booking_id = b.id
		LEFT JOIN refunds r ON r.booking_id = b.id
		WHERE s.store_id = ? AND s.start_at >= ? AND s.start_at < ?
		GROUP BY b.status`, storeID, from.UTC(), to.UTC())
	if err != nil {
		return reporting.DailySummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		var collected, refunded, pending int64
		if err := rows.Scan(&status, &count, &collected, &refunded, &pending); err != nil {
			return reporting.DailySummary{}, err
		}
		result.BookingCount += count
		result.StatusCounts[status] += count
		result.CollectedCents += collected
		result.RefundedCents += refunded
		result.PendingRefundCents += pending
	}
	if err := rows.Err(); err != nil {
		return reporting.DailySummary{}, err
	}
	result.OutstandingCents = result.CollectedCents - result.RefundedCents
	return result, nil
}

func bookingStatus(value string) booking.Status { return booking.Status(value) }
