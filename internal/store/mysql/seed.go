package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

var teachingAccounts = []struct {
	id      int64
	name    string
	role    string
	ownerID int64
}{
	{1, "owner1", "owner", 1}, {2, "owner2", "owner", 2},
	{3, "owner3", "owner", 3}, {4, "owner4", "owner", 4},
	{5, "frontdesk", "desk", 0}, {6, "groomer", "worker", 0},
	{7, "support", "support", 0}, {8, "manager", "manager", 0},
}

func (s *Store) SeedTeachingData(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO stores (id, name) VALUES (1, '宠物门店')
		ON DUPLICATE KEY UPDATE id = id`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects (id, store_id, name, price_cents)
		VALUES (1, 1, '洗护', 9900) ON DUPLICATE KEY UPDATE id = id`); err != nil {
		return err
	}
	for _, account := range teachingAccounts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO accounts (id, username, password_hash, role, store_id, owner_id)
			VALUES (?, ?, '', ?, 1, ?) ON DUPLICATE KEY UPDATE id = id`,
			account.id, account.name, account.role, account.ownerID); err != nil {
			return err
		}
	}
	for _, pet := range []struct {
		id   int64
		name string
	}{
		{1, "团团"}, {2, "豆豆"}, {3, "雪球"}, {4, "米粒"},
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pets (id, owner_id, store_id, name)
			VALUES (?, ?, 1, ?) ON DUPLICATE KEY UPDATE id = id`, pet.id, pet.id, pet.name); err != nil {
			return err
		}
	}
	for i := int64(0); i < 3; i++ {
		start := now.UTC().Truncate(time.Hour).Add(time.Duration(4+i) * time.Hour)
		if _, err := tx.ExecContext(ctx, `INSERT INTO slots (id, store_id, project_id, start_at, end_at, capacity)
			VALUES (?, 1, 1, ?, ?, 3) ON DUPLICATE KEY UPDATE id = id`, i+1, start, start.Add(time.Hour)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ProvisionPassword(ctx context.Context, username string, password []byte) error {
	if len(password) == 0 {
		return errors.New("password must not be empty")
	}
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var accountID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE username = ? FOR UPDATE`, username).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET password_hash = ? WHERE id = ?`, hash, accountID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE account_id = ?`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UnconfiguredAccounts(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, password_hash, role, store_id, owner_id FROM accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	count := 0
	for rows.Next() {
		var id, storeID, ownerID int64
		var name, hash, role string
		if err := rows.Scan(&id, &name, &hash, &role, &storeID, &ownerID); err != nil {
			return nil, err
		}
		if count >= len(teachingAccounts) {
			return nil, errors.New("unexpected teaching account")
		}
		expected := teachingAccounts[count]
		if id != expected.id || name != expected.name || role != expected.role || storeID != 1 || ownerID != expected.ownerID {
			return nil, errors.New("teaching account does not match preset identity")
		}
		if hash == "" {
			names = append(names, name)
		} else if _, err := bcrypt.Cost([]byte(hash)); err != nil {
			return nil, errors.New("teaching account has an invalid password hash")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if count != len(teachingAccounts) {
		return nil, errors.New("teaching accounts are incomplete")
	}
	for _, item := range []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM stores WHERE id = 1`, 1},
		{`SELECT COUNT(*) FROM projects WHERE id = 1 AND store_id = 1 AND price_cents = 9900`, 1},
		{`SELECT COUNT(*) FROM pets WHERE id BETWEEN 1 AND 4 AND owner_id = id AND store_id = 1`, 4},
		{`SELECT COUNT(*) FROM slots WHERE id BETWEEN 1 AND 3 AND store_id = 1 AND project_id = 1
			AND capacity = 3 AND start_at < end_at`, 3},
	} {
		var got int
		if err := s.db.QueryRowContext(ctx, item.query).Scan(&got); err != nil {
			return nil, err
		}
		if got != item.want {
			return nil, errors.New("teaching data is incomplete")
		}
	}
	return names, nil
}
