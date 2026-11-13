package mysqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestClosedDatabaseReturnsErrorsWithoutAcceptingOperations(t *testing.T) {
	db, err := sql.Open("mysql", "user@tcp(127.0.0.1:3306)/unused")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	ctx := context.Background()
	digest := sha256.Sum256([]byte("fixture"))
	checks := map[string]func() error{
		"migration":        func() error { return store.Migrate(ctx) },
		"seed":             func() error { return store.SeedTeachingData(ctx, time.Now()) },
		"account":          func() error { _, err := store.AccountByUsername(ctx, "owner1"); return err },
		"session write":    func() error { return store.SaveSession(ctx, digest, 1, "fixture-hash", time.Now()) },
		"session read":     func() error { _, err := store.SessionByDigest(ctx, digest); return err },
		"session deletion": func() error { return store.DeleteSession(ctx, digest) },
		"clock read":       func() error { _, err := store.ClockNow(ctx, time.Now()); return err },
		"clock advance":    func() error { return store.AdvanceClock(ctx, 1) },
		"provision":        func() error { return store.ProvisionPassword(ctx, "owner1", []byte("fixture-only")) },
		"readiness":        func() error { _, err := store.UnconfiguredAccounts(ctx); return err },
	}
	for name, check := range checks {
		if err := check(); err == nil {
			t.Errorf("%s succeeded with closed database", name)
		}
	}
}
