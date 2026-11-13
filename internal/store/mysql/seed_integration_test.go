package mysqlstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

type gatedSessionRepository struct {
	*Store
	enter   chan struct{}
	release chan struct{}
}

func (r *gatedSessionRepository) SaveSession(ctx context.Context, digest [32]byte, accountID int64, hash string, expires time.Time) error {
	close(r.enter)
	<-r.release
	return r.Store.SaveSession(ctx, digest, accountID, hash, expires)
}

func TestTeachingSeedIsIdempotentAndPasswordsAreProvisionedSeparately(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnconfiguredAccounts(ctx); err == nil {
		t.Fatal("empty account table must not be considered ready to serve")
	}
	base := time.Date(2026, 11, 13, 8, 15, 0, 0, time.UTC)
	if err := store.SeedTeachingData(ctx, base); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"stores": 1, "projects": 1, "pets": 4, "slots": 3, "accounts": 8} {
		var got int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s count=%d error=%v, want %d", table, got, err, want)
		}
	}
	var start time.Time
	if err := db.QueryRowContext(ctx, "SELECT start_at FROM slots WHERE id = 1").Scan(&start); err != nil || !start.After(base) {
		t.Fatalf("first slot start=%s error=%v", start, err)
	}
	if _, err := auth.NewService(store, time.Now).Login(ctx, "owner1", "unconfigured"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("unprovisioned account error=%v", err)
	}
	unconfigured, err := store.UnconfiguredAccounts(ctx)
	if err != nil || len(unconfigured) != 8 {
		t.Fatalf("unconfigured count=%d error=%v, want 8", len(unconfigured), err)
	}
	if err := store.ProvisionPassword(ctx, "owner1", []byte("owner-test-password")); err != nil {
		t.Fatal(err)
	}
	if err := store.ProvisionPassword(ctx, "frontdesk", []byte("desk-test-password")); err != nil {
		t.Fatal(err)
	}
	unconfigured, err = store.UnconfiguredAccounts(ctx)
	if err != nil || len(unconfigured) != 6 {
		t.Fatalf("unconfigured count=%d error=%v, want 6", len(unconfigured), err)
	}
	if err := store.SeedTeachingData(ctx, base.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var startAgain time.Time
	if err := db.QueryRowContext(ctx, "SELECT start_at FROM slots WHERE id = 1").Scan(&startAgain); err != nil || !startAgain.Equal(start) {
		t.Fatalf("seed reset first slot=%s error=%v", startAgain, err)
	}
	svc := auth.NewService(store, time.Now)
	for _, tc := range []struct {
		name, password, role string
		ownerID              int64
	}{
		{"owner1", "owner-test-password", "owner", 1},
		{"frontdesk", "desk-test-password", "desk", 0},
	} {
		token, err := svc.Login(ctx, tc.name, tc.password)
		if err != nil {
			t.Fatalf("%s login: %v", tc.name, err)
		}
		identity, err := svc.Authenticate(ctx, token)
		if err != nil || identity.Role != tc.role || identity.OwnerID != tc.ownerID || identity.StoreID != 1 {
			t.Fatalf("%s role=%s owner=%d store=%d error=%v", tc.name, identity.Role, identity.OwnerID, identity.StoreID, err)
		}
	}
	if _, err := svc.Login(ctx, "owner1", "desk-test-password"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("other account password accepted: %v", err)
	}
	oldToken, err := svc.Login(ctx, "owner1", "owner-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ProvisionPassword(ctx, "owner1", []byte("replacement-password")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, oldToken); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("old session survived password replacement: %v", err)
	}
	if _, err := svc.Login(ctx, "owner1", "owner-test-password"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("old password accepted: %v", err)
	}
	if _, err := svc.Login(ctx, "owner1", "replacement-password"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
	if err := store.ProvisionPassword(ctx, "missing", []byte("long-enough-password")); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("unknown account provisioning error=%v", err)
	}
	if err := store.ProvisionPassword(ctx, "owner1", nil); err == nil {
		t.Fatal("empty password accepted")
	}
	var price int64
	if err := db.QueryRowContext(ctx, "SELECT price_cents FROM projects WHERE id = 1").Scan(&price); err != nil || price != 9900 {
		t.Fatalf("price=%d error=%v", price, err)
	}
}

func TestIncompleteTeachingDataCannotServe(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("fixture-only"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts
		(id, username, password_hash, role, store_id, owner_id)
		VALUES (1, 'owner1', ?, 'owner', 1, 1)`, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnconfiguredAccounts(ctx); err == nil {
		t.Fatal("single configured account was considered complete teaching data")
	}
	if err := store.SeedTeachingData(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET role = 'manager' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnconfiguredAccounts(ctx); err == nil {
		t.Fatal("wrong preset role was considered ready")
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET role = 'owner' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM pets WHERE id = 4`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnconfiguredAccounts(ctx); err == nil {
		t.Fatal("missing preset pet was considered ready")
	}
	if err := store.SeedTeachingData(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE slots SET capacity = 2 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnconfiguredAccounts(ctx); err == nil {
		t.Fatal("wrong preset capacity was considered ready")
	}
	if _, err := db.ExecContext(ctx, `UPDATE slots SET capacity = 3 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET password_hash = 'invalid-hash' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UnconfiguredAccounts(ctx); err == nil {
		t.Fatal("invalid password hash was considered ready")
	}
}

func TestPasswordResetBetweenVerificationAndSessionWriteRejectsOldLogin(t *testing.T) {
	db, _ := testDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.SeedTeachingData(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.ProvisionPassword(ctx, "owner1", []byte("old-fixture-password")); err != nil {
		t.Fatal(err)
	}
	gated := &gatedSessionRepository{Store: store, enter: make(chan struct{}), release: make(chan struct{})}
	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		token, err := auth.NewService(gated, time.Now).Login(ctx, "owner1", "old-fixture-password")
		done <- result{token, err}
	}()
	select {
	case <-gated.enter:
	case <-ctx.Done():
		t.Fatal("login never reached session write")
	}
	if err := store.ProvisionPassword(ctx, "owner1", []byte("new-fixture-password")); err != nil {
		t.Fatal(err)
	}
	close(gated.release)
	select {
	case outcome := <-done:
		if outcome.token != "" || !errors.Is(outcome.err, auth.ErrUnauthorized) {
			t.Fatalf("old login token created=%v error=%v", outcome.token != "", outcome.err)
		}
	case <-ctx.Done():
		t.Fatal("login did not complete")
	}
	var sessions int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("old sessions=%d error=%v", sessions, err)
	}
}
