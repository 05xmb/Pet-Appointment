package reporting

import (
	"context"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
)

type serviceRepository struct{}

func (serviceRepository) ListBookings(context.Context, auth.Identity, time.Time, time.Time) ([]BookingView, error) {
	return []BookingView{{ID: 1}}, nil
}
func (serviceRepository) DailySummary(context.Context, int64, time.Time, time.Time, string) (DailySummary, error) {
	return DailySummary{BookingCount: 1}, nil
}

func TestServiceDelegatesValidReports(t *testing.T) {
	service := NewService(serviceRepository{})
	ctx := context.Background()
	from, to := time.Now(), time.Now().Add(time.Hour)
	views, err := service.ListBookings(ctx, auth.Identity{ID: 1, Role: "owner", StoreID: 1, OwnerID: 1}, from, to)
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v error=%v", views, err)
	}
	summary, err := service.DailySummary(ctx, auth.Identity{ID: 8, Role: "manager", StoreID: 1}, from, to, "2026-11-13")
	if err != nil || summary.BookingCount != 1 {
		t.Fatalf("summary=%+v error=%v", summary, err)
	}
}

func TestServiceRejectsInvalidReportIdentities(t *testing.T) {
	service := NewService(serviceRepository{})
	ctx := context.Background()
	now := time.Now()
	for _, identity := range []auth.Identity{{Role: "owner", StoreID: 1}, {ID: 1, Role: "unknown", StoreID: 1, OwnerID: 1}} {
		if _, err := service.ListBookings(ctx, identity, now, now.Add(time.Hour)); err != ErrForbidden {
			t.Fatalf("identity=%+v error=%v", identity, err)
		}
	}
	if _, err := service.DailySummary(ctx, auth.Identity{ID: 1, Role: "owner", StoreID: 1, OwnerID: 1}, now, now.Add(time.Hour), "2026-11-13"); err != ErrForbidden {
		t.Fatalf("owner daily error=%v", err)
	}
}
