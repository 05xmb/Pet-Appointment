package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/slot"
)

func (s *Store) Create(ctx context.Context, actorID, storeID, projectID int64, start, end time.Time, capacity int) (slot.Slot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return slot.Slot{}, err
	}
	defer tx.Rollback()
	var projectStore int64
	if err := tx.QueryRowContext(ctx, `SELECT store_id FROM projects WHERE id = ? FOR UPDATE`, projectID).Scan(&projectStore); errors.Is(err, sql.ErrNoRows) {
		return slot.Slot{}, slot.ErrNotFound
	} else if err != nil {
		return slot.Slot{}, err
	}
	if projectStore != storeID {
		return slot.Slot{}, slot.ErrForbidden
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT next_id FROM slot_sequence WHERE id = 1 FOR UPDATE`).Scan(&id); err != nil {
		return slot.Slot{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE slot_sequence SET next_id = next_id + 1 WHERE id = 1`); err != nil {
		return slot.Slot{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO slots (id, store_id, project_id, start_at, end_at, capacity) VALUES (?, ?, ?, ?, ?, ?)`, id, storeID, projectID, start.UTC(), end.UTC(), capacity); err != nil {
		return slot.Slot{}, err
	}
	if err := writeAudit(ctx, tx, actorID, "slot", id, "created", "", "AVAILABLE", ""); err != nil {
		return slot.Slot{}, err
	}
	if err := tx.Commit(); err != nil {
		return slot.Slot{}, err
	}
	return slot.Slot{ID: id, StoreID: storeID, ProjectID: projectID, StartAt: start, EndAt: end, Capacity: capacity}, nil
}

func (s *Store) List(ctx context.Context, storeID int64, from, to time.Time) ([]slot.Slot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, store_id, project_id, start_at, end_at, capacity FROM slots WHERE store_id = ? AND start_at >= ? AND start_at < ? ORDER BY start_at, id`, storeID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []slot.Slot
	for rows.Next() {
		var item slot.Slot
		if err := rows.Scan(&item.ID, &item.StoreID, &item.ProjectID, &item.StartAt, &item.EndAt, &item.Capacity); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
