package mysqlstore

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestTeachingClockPersistsOffsetAcrossMigrateAndReconnect(t *testing.T) {
	db, dsn := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 13, 15, 58, 0, 0, time.UTC)
	if got, err := store.ClockNow(ctx, base); err != nil || !got.Equal(base) {
		t.Fatalf("initial clock=%s error=%v", got, err)
	}
	if err := store.AdvanceClock(ctx, 5); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := New(reopened).ClockNow(ctx, base)
	if err != nil || !got.Equal(base.Add(5*time.Minute)) {
		t.Fatalf("reopened clock=%s error=%v", got, err)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if date := got.In(shanghai).Format("2006-01-02"); date != "2026-11-14" {
		t.Fatalf("Shanghai business date=%s", date)
	}
	if err := New(reopened).AdvanceClock(ctx, 0); err == nil {
		t.Fatal("zero-minute advance should fail")
	}
}

func TestTeachingClockRejectsCumulativeDurationOverflow(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	nearLimit := int64((1<<63-1)/int64(time.Second)) - 30
	if _, err := db.ExecContext(ctx, `UPDATE app_clock SET offset_seconds = ? WHERE id = 1`, nearLimit); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	before, err := store.ClockNow(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AdvanceClock(ctx, 1); err == nil {
		t.Fatal("cumulative advance overflow was accepted")
	}
	after, err := store.ClockNow(ctx, base)
	if err != nil || !after.Equal(before) {
		t.Fatalf("rejected advance changed clock: before=%s after=%s error=%v", before, after, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE app_clock SET offset_seconds = -1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClockNow(ctx, base); err == nil {
		t.Fatal("negative clock offset was accepted")
	}
}
