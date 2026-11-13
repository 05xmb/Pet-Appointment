package mysqlstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/slot"
)

func TestManagerCanCreateAndListFutureSlots(t *testing.T) {
	db, _ := testDatabase(t)
	ctx := context.Background()
	store := New(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	if err := store.SeedTeachingData(ctx, base); err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(ctx, 8, 1, 1, base.Add(8*time.Hour), base.Add(9*time.Hour), 2)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 4 || created.Capacity != 2 {
		t.Fatalf("created=%+v", created)
	}
	items, err := store.List(ctx, 1, base.Add(7*time.Hour), base.Add(10*time.Hour))
	if err != nil || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("items=%+v error=%v", items, err)
	}
	if _, err := store.Create(ctx, 8, 2, 1, base.Add(10*time.Hour), base.Add(11*time.Hour), 2); !errors.Is(err, slot.ErrForbidden) {
		t.Fatalf("cross-store project error=%v", err)
	}
}
