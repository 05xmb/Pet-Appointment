package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Migrate(ctx context.Context) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS accounts (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			username VARCHAR(100) NOT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			role VARCHAR(16) NOT NULL,
			store_id BIGINT NOT NULL,
			owner_id BIGINT NOT NULL DEFAULT 0
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_digest BINARY(32) NOT NULL PRIMARY KEY,
			account_id BIGINT NOT NULL,
			expires_at DATETIME(6) NOT NULL,
			INDEX sessions_expires_at (expires_at),
			CONSTRAINT sessions_account_fk FOREIGN KEY (account_id) REFERENCES accounts(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS app_clock (
			id TINYINT NOT NULL PRIMARY KEY,
			offset_seconds BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`INSERT INTO app_clock (id, offset_seconds) VALUES (1, 0)
			ON DUPLICATE KEY UPDATE id = id`,
		`CREATE TABLE IF NOT EXISTS stores (
			id BIGINT NOT NULL PRIMARY KEY,
			name VARCHAR(100) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS projects (
			id BIGINT NOT NULL PRIMARY KEY,
			store_id BIGINT NOT NULL,
			name VARCHAR(100) NOT NULL,
			price_cents BIGINT NOT NULL,
			CONSTRAINT projects_store_fk FOREIGN KEY (store_id) REFERENCES stores(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS pets (
			id BIGINT NOT NULL PRIMARY KEY,
			owner_id BIGINT NOT NULL,
			store_id BIGINT NOT NULL,
			name VARCHAR(100) NOT NULL,
			CONSTRAINT pets_owner_fk FOREIGN KEY (owner_id) REFERENCES accounts(id),
			CONSTRAINT pets_store_fk FOREIGN KEY (store_id) REFERENCES stores(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS slots (
			id BIGINT NOT NULL PRIMARY KEY,
			store_id BIGINT NOT NULL,
			project_id BIGINT NOT NULL,
			start_at DATETIME(6) NOT NULL,
			end_at DATETIME(6) NOT NULL,
			capacity INT NOT NULL,
			CONSTRAINT slots_store_fk FOREIGN KEY (store_id) REFERENCES stores(id),
			CONSTRAINT slots_project_fk FOREIGN KEY (project_id) REFERENCES projects(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS slot_sequence (
			id TINYINT NOT NULL PRIMARY KEY,
			next_id BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`INSERT INTO slot_sequence (id, next_id)
			SELECT 1, GREATEST(COALESCE(MAX(id), 0) + 1, 4) FROM slots
			ON DUPLICATE KEY UPDATE next_id = GREATEST(next_id, VALUES(next_id))`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			actor_id BIGINT NOT NULL DEFAULT 0,
			entity_type VARCHAR(32) NOT NULL,
			entity_id BIGINT NOT NULL,
			action VARCHAR(48) NOT NULL,
			from_state VARCHAR(32) NOT NULL DEFAULT '',
			to_state VARCHAR(32) NOT NULL DEFAULT '',
			detail VARCHAR(255) NOT NULL DEFAULT '',
			created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			INDEX audit_entity_time (entity_type, entity_id, created_at),
			INDEX audit_actor_time (actor_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS bookings (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			slot_id BIGINT NOT NULL,
			pet_id BIGINT NOT NULL,
			owner_id BIGINT NOT NULL,
			status VARCHAR(32) NOT NULL,
			expires_at DATETIME(6) NOT NULL,
			created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
			INDEX bookings_slot_status (slot_id, status),
			INDEX bookings_pet_slot (pet_id, slot_id, status),
			CONSTRAINT bookings_slot_fk FOREIGN KEY (slot_id) REFERENCES slots(id),
			CONSTRAINT bookings_pet_fk FOREIGN KEY (pet_id) REFERENCES pets(id),
			CONSTRAINT bookings_owner_fk FOREIGN KEY (owner_id) REFERENCES accounts(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS payments (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			booking_id BIGINT NOT NULL UNIQUE,
			owner_id BIGINT NOT NULL,
			amount_cents BIGINT NOT NULL,
			status VARCHAR(16) NOT NULL,
			created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
			CONSTRAINT payments_booking_fk FOREIGN KEY (booking_id) REFERENCES bookings(id),
			CONSTRAINT payments_owner_fk FOREIGN KEY (owner_id) REFERENCES accounts(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS exceptions (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			payment_id BIGINT NOT NULL,
			booking_id BIGINT NOT NULL,
			type VARCHAR(32) NOT NULL,
			status VARCHAR(32) NOT NULL,
			amount_cents BIGINT NOT NULL,
			detail VARCHAR(255) NOT NULL,
			created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			UNIQUE KEY exceptions_payment_type (payment_id, type),
			CONSTRAINT exceptions_payment_fk FOREIGN KEY (payment_id) REFERENCES payments(id),
			CONSTRAINT exceptions_booking_fk FOREIGN KEY (booking_id) REFERENCES bookings(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS cancellation_requests (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			booking_id BIGINT NOT NULL UNIQUE,
			owner_id BIGINT NOT NULL,
			reason VARCHAR(255) NOT NULL,
			status VARCHAR(16) NOT NULL,
			reviewed_by BIGINT NOT NULL DEFAULT 0,
			created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			reviewed_at DATETIME(6) NULL,
			CONSTRAINT cancellation_booking_fk FOREIGN KEY (booking_id) REFERENCES bookings(id),
			CONSTRAINT cancellation_owner_fk FOREIGN KEY (owner_id) REFERENCES accounts(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS refunds (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			booking_id BIGINT NOT NULL UNIQUE,
			payment_id BIGINT NOT NULL,
			amount_cents BIGINT NOT NULL,
			status VARCHAR(16) NOT NULL,
			created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
			CONSTRAINT refunds_booking_fk FOREIGN KEY (booking_id) REFERENCES bookings(id),
			CONSTRAINT refunds_payment_fk FOREIGN KEY (payment_id) REFERENCES payments(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS service_events (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			booking_id BIGINT NOT NULL,
			actor_id BIGINT NOT NULL,
			type VARCHAR(32) NOT NULL,
			reason VARCHAR(255) NOT NULL DEFAULT '',
			occurred_at DATETIME(6) NOT NULL,
			UNIQUE KEY service_events_booking_type (booking_id, type),
			INDEX service_events_booking_time (booking_id, occurred_at),
			CONSTRAINT service_events_booking_fk FOREIGN KEY (booking_id) REFERENCES bookings(id),
			CONSTRAINT service_events_actor_fk FOREIGN KEY (actor_id) REFERENCES accounts(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AccountByUsername(ctx context.Context, username string) (auth.Account, error) {
	var account auth.Account
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, store_id, owner_id
		FROM accounts WHERE username = ?`, username).Scan(
		&account.ID, &account.Username, &account.PasswordHash, &account.Role, &account.StoreID, &account.OwnerID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Account{}, auth.ErrNotFound
	}
	return account, err
}

func (s *Store) SaveSession(ctx context.Context, digest [32]byte, accountID int64, verifiedHash string, expires time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentHash string
	err = tx.QueryRowContext(ctx, `SELECT password_hash FROM accounts WHERE id = ? FOR UPDATE`, accountID).Scan(&currentHash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && currentHash != verifiedHash) {
		return auth.ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (token_digest, account_id, expires_at)
		VALUES (?, ?, ?)`, digest[:], accountID, expires.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SessionByDigest(ctx context.Context, digest [32]byte) (auth.Session, error) {
	var session auth.Session
	err := s.db.QueryRowContext(ctx, `SELECT a.id, a.username, a.password_hash, a.role, a.store_id, a.owner_id, s.expires_at
		FROM sessions s JOIN accounts a ON a.id = s.account_id WHERE s.token_digest = ?`, digest[:]).Scan(
		&session.Account.ID, &session.Account.Username, &session.Account.PasswordHash,
		&session.Account.Role, &session.Account.StoreID, &session.Account.OwnerID, &session.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Session{}, auth.ErrNotFound
	}
	return session, err
}

func (s *Store) DeleteSession(ctx context.Context, digest [32]byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_digest = ?`, digest[:])
	return err
}
