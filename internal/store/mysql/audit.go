package mysqlstore

import (
	"context"
	"database/sql"
)

func writeAudit(ctx context.Context, tx *sql.Tx, actorID int64, entityType string, entityID int64, action, fromState, toState, detail string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_logs
		(actor_id, entity_type, entity_id, action, from_state, to_state, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, actorID, entityType, entityID, action, fromState, toState, detail)
	return err
}
