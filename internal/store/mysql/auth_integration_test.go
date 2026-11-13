package mysqlstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

func testDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("PET_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("PET_MYSQL_TEST_DSN not set; MySQL integration test not run")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.Ping(); err != nil {
		t.Fatal("test MySQL connection failed")
	}
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "pet_appointment_test_" + hex.EncodeToString(suffix[:])
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Errorf("could not remove isolated test database: %v", err)
		}
	})
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, cfg.FormatDSN()
}

func TestAccountsAndSessionsSurviveDatabaseReconnect(t *testing.T) {
	db, dsn := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("fixture-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO accounts (id, username, password_hash, role, store_id, owner_id) VALUES (7, 'owner', ?, 'owner', 1, 7)", hash); err != nil {
		t.Fatal(err)
	}
	account, err := store.AccountByUsername(ctx, "owner")
	if err != nil || account.ID != 7 || account.Role != "owner" || account.OwnerID != 7 || account.PasswordHash != string(hash) {
		t.Fatalf("account id=%d role=%q owner=%d hash matched=%v error=%v", account.ID, account.Role, account.OwnerID, account.PasswordHash == string(hash), err)
	}
	if _, err := store.AccountByUsername(ctx, "missing"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("unknown account error=%v", err)
	}
	digest := sha256.Sum256([]byte("only-this-digest-is-persisted"))
	expires := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	if err := store.SaveSession(ctx, digest, 7, string(hash), expires); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	store = New(reopened)
	session, err := store.SessionByDigest(ctx, digest)
	if err != nil || session.Account.ID != 7 || !session.ExpiresAt.Equal(expires) {
		t.Fatalf("reopened session account=%d expires=%s error=%v", session.Account.ID, session.ExpiresAt, err)
	}
	if err := store.DeleteSession(ctx, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SessionByDigest(ctx, digest); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("revoked session error=%v", err)
	}
}
