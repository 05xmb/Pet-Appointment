package mysqlstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/refund"
	"github.com/05xmb/Pet-Appointment/internal/slot"
	"github.com/DATA-DOG/go-sqlmock"
)

func mockStore(t *testing.T) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet SQL expectation: %v", err)
		}
		_ = db.Close()
	})
	return New(db), mock
}

func expectAudit(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT INTO audit_logs").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func bookingRow(id, slotID, petID, ownerID int64, status booking.Status, expiresAt, createdAt time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "slot_id", "pet_id", "owner_id", "status", "expires_at", "created_at", "updated_at"}).
		AddRow(id, slotID, petID, ownerID, status, expiresAt, createdAt, createdAt)
}

func expectCreatePendingPrefix(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT store_id, start_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store_id", "start_at"}).AddRow(1, now.Add(time.Hour)))
	mock.ExpectQuery("SELECT id FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT owner_id FROM pets").WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT capacity FROM slots").WillReturnRows(sqlmock.NewRows([]string{"capacity"}).AddRow(3))
}

func expectPaymentCallbackPrefix(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT p.id, p.booking_id, p.owner_id, p.amount_cents, p.status").WillReturnRows(
		sqlmock.NewRows([]string{"id", "booking", "owner", "amount", "status", "created", "updated", "booking_status", "expires"}).
			AddRow(20, 10, 1, 9900, "PENDING", now, now, "PENDING_PAYMENT", now.Add(time.Minute)),
	)
}

func expectSlotCreatePrefix(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT store_id FROM projects").WillReturnRows(sqlmock.NewRows([]string{"store_id"}).AddRow(1))
	mock.ExpectQuery("SELECT next_id FROM slot_sequence").WillReturnRows(sqlmock.NewRows([]string{"next_id"}).AddRow(4))
}

func expectReviewPrefix(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT role FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("support"))
	mock.ExpectQuery("SELECT booking_id, status FROM cancellation_requests").WillReturnRows(sqlmock.NewRows([]string{"booking_id", "status"}).AddRow(10, "PENDING"))
	mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusCancelReview, now, now))
	mock.ExpectQuery("SELECT id, amount_cents, status FROM payments").WillReturnRows(sqlmock.NewRows([]string{"id", "amount", "status"}).AddRow(20, 9900, "SUCCESS"))
}

func expectCancellationRequestPrefix(mock sqlmock.Sqlmock, now time.Time) {
	mock.ExpectBegin()
	expectAudit(mock)
	mock.ExpectQuery("SELECT b.status, b.owner_id, s.start_at").WillReturnRows(sqlmock.NewRows([]string{"status", "owner", "start"}).AddRow("CONFIRMED", 1, now.Add(time.Hour)))
}

func TestSchemaSeedAndIdentityRepositoriesWithoutMySQL(t *testing.T) {
	t.Run("migration", func(t *testing.T) {
		store, mock := mockStore(t)
		for range 17 {
			mock.ExpectExec("(?s).+").WillReturnResult(sqlmock.NewResult(0, 0))
		}
		if err := store.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("seed", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		for range 17 {
			mock.ExpectExec("(?s).+").WillReturnResult(sqlmock.NewResult(1, 1))
		}
		mock.ExpectCommit()
		if err := store.SeedTeachingData(context.Background(), time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("account and session", func(t *testing.T) {
		store, mock := mockStore(t)
		now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
		mock.ExpectQuery("SELECT id, username, password_hash, role, store_id, owner_id").
			WithArgs("owner1").
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "store_id", "owner_id"}).AddRow(1, "owner1", "hash", "owner", 1, 1))
		account, err := store.AccountByUsername(context.Background(), "owner1")
		if err != nil || account.ID != 1 || account.OwnerID != 1 {
			t.Fatalf("account=%+v error=%v", account, err)
		}

		digest := sha256.Sum256([]byte("token"))
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT password_hash FROM accounts").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow("hash"))
		mock.ExpectExec("INSERT INTO sessions").WithArgs(digest[:], int64(1), now).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		if err := store.SaveSession(context.Background(), digest, 1, "hash", now); err != nil {
			t.Fatal(err)
		}

		mock.ExpectQuery("SELECT a.id, a.username, a.password_hash, a.role, a.store_id, a.owner_id, s.expires_at").
			WithArgs(digest[:]).
			WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "store_id", "owner_id", "expires_at"}).AddRow(1, "owner1", "hash", "owner", 1, 1, now))
		session, err := store.SessionByDigest(context.Background(), digest)
		if err != nil || session.Account.ID != 1 || !session.ExpiresAt.Equal(now) {
			t.Fatalf("session=%+v error=%v", session, err)
		}

		mock.ExpectExec("DELETE FROM sessions").WithArgs(digest[:]).WillReturnResult(sqlmock.NewResult(0, 1))
		if err := store.DeleteSession(context.Background(), digest); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("password and readiness", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM accounts").WithArgs("owner1").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectExec("UPDATE accounts SET password_hash").WithArgs(sqlmock.AnyArg(), int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("DELETE FROM sessions").WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		if err := store.ProvisionPassword(context.Background(), "owner1", []byte("unit-password")); err != nil {
			t.Fatal(err)
		}

		rows := sqlmock.NewRows([]string{"id", "username", "password_hash", "role", "store_id", "owner_id"})
		for _, item := range teachingAccounts {
			rows.AddRow(item.id, item.name, "", item.role, 1, item.ownerID)
		}
		mock.ExpectQuery("SELECT id, username, password_hash, role, store_id, owner_id FROM accounts").WillReturnRows(rows)
		for _, count := range []int{1, 1, 4, 3} {
			mock.ExpectQuery("SELECT COUNT\\(\\*\\)").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
		}
		names, err := store.UnconfiguredAccounts(context.Background())
		if err != nil || len(names) != len(teachingAccounts) {
			t.Fatalf("names=%v error=%v", names, err)
		}
	})
}

func TestClockSlotAndReportingRepositoriesWithoutMySQL(t *testing.T) {
	t.Run("clock", func(t *testing.T) {
		store, mock := mockStore(t)
		realNow := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
		mock.ExpectQuery("SELECT offset_seconds FROM app_clock").WillReturnRows(sqlmock.NewRows([]string{"offset_seconds"}).AddRow(120))
		got, err := store.ClockNow(context.Background(), realNow)
		if err != nil || !got.Equal(realNow.Add(2*time.Minute)) {
			t.Fatalf("now=%v error=%v", got, err)
		}
		mock.ExpectExec("UPDATE app_clock SET offset_seconds").WithArgs(int64(60), maxClockOffsetSeconds-60).WillReturnResult(sqlmock.NewResult(0, 1))
		if err := store.AdvanceClock(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		if err := store.AdvanceClock(context.Background(), 0); err == nil {
			t.Fatal("zero clock advance accepted")
		}
	})

	t.Run("slot", func(t *testing.T) {
		store, mock := mockStore(t)
		start := time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC)
		end := start.Add(time.Hour)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id FROM projects").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"store_id"}).AddRow(1))
		mock.ExpectQuery("SELECT next_id FROM slot_sequence").WillReturnRows(sqlmock.NewRows([]string{"next_id"}).AddRow(4))
		mock.ExpectExec("UPDATE slot_sequence").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO slots").WithArgs(int64(4), int64(1), int64(1), start, end, 3).WillReturnResult(sqlmock.NewResult(4, 1))
		expectAudit(mock)
		mock.ExpectCommit()
		created, err := store.Create(context.Background(), 8, 1, 1, start, end, 3)
		if err != nil || created.ID != 4 || created.Capacity != 3 {
			t.Fatalf("slot=%+v error=%v", created, err)
		}

		mock.ExpectQuery("SELECT id, store_id, project_id, start_at, end_at, capacity FROM slots").
			WithArgs(int64(1), start, end).
			WillReturnRows(sqlmock.NewRows([]string{"id", "store_id", "project_id", "start_at", "end_at", "capacity"}).AddRow(4, 1, 1, start, end, 3))
		items, err := store.List(context.Background(), 1, start, end)
		if err != nil || len(items) != 1 || items[0].ID != 4 {
			t.Fatalf("slots=%+v error=%v", items, err)
		}
	})

	t.Run("reporting", func(t *testing.T) {
		store, mock := mockStore(t)
		from := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
		to := from.Add(24 * time.Hour)
		mock.ExpectQuery("SELECT b.id, b.slot_id, b.pet_id, b.owner_id, b.status").
			WithArgs(int64(1), from, to, int64(1)).
			WillReturnRows(sqlmock.NewRows([]string{"id", "slot_id", "pet_id", "owner_id", "status", "start_at", "end_at", "payment", "cancellation", "refund", "events"}).
				AddRow(2, 1, 1, 1, "COMPLETED", from.Add(time.Hour), from.Add(2*time.Hour), "SUCCESS", "", "", "CHECK_IN,SERVICE_START,SERVICE_DONE"))
		views, err := store.ListBookings(context.Background(), auth.Identity{Role: "owner", StoreID: 1, OwnerID: 1}, from, to)
		if err != nil || len(views) != 1 || views[0].Status != booking.StatusCompleted || len(views[0].ServiceEvents) != 3 {
			t.Fatalf("views=%+v error=%v", views, err)
		}

		mock.ExpectQuery("SELECT b.status, COUNT\\(\\*\\)").WithArgs(int64(1), from, to).
			WillReturnRows(sqlmock.NewRows([]string{"status", "count", "collected", "refunded", "pending"}).AddRow("COMPLETED", 1, 9900, 0, 0))
		summary, err := store.DailySummary(context.Background(), 1, from, to, "2026-11-14")
		if err != nil || summary.BookingCount != 1 || summary.CollectedCents != 9900 || summary.OutstandingCents != 9900 || summary.StatusCounts["COMPLETED"] != 1 {
			t.Fatalf("summary=%+v error=%v", summary, err)
		}
	})
}

func TestBookingRepositoriesWithoutMySQL(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	store, mock := mockStore(t)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT store_id, start_at FROM slots").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"store_id", "start_at"}).AddRow(1, now.Add(4*time.Hour)))
	mock.ExpectQuery("SELECT id FROM bookings").WithArgs(int64(1), now).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectExec("UPDATE bookings SET status = 'CLOSED'").WithArgs(int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectQuery("SELECT owner_id FROM pets").WithArgs(int64(1), int64(1)).WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM bookings").WithArgs(int64(1), int64(1)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM bookings").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT capacity FROM slots").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"capacity"}).AddRow(3))
	mock.ExpectExec("INSERT INTO bookings").WithArgs(int64(1), int64(1), int64(1), now.Add(5*time.Minute)).WillReturnResult(sqlmock.NewResult(10, 1))
	mock.ExpectQuery("SELECT created_at, updated_at FROM bookings").WithArgs(int64(10)).WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))
	expectAudit(mock)
	mock.ExpectCommit()
	created, err := store.CreatePending(ctx, 1, 1, 1, now)
	if err != nil || created.ID != 10 || created.Status != booking.StatusPendingPayment {
		t.Fatalf("created=%+v error=%v", created, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WithArgs(int64(10)).WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusPendingPayment, now, now))
	mock.ExpectExec("UPDATE bookings SET status = 'CLOSED'").WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectCommit()
	closed, err := store.CloseExpired(ctx, 10, now)
	if err != nil || closed.Status != booking.StatusClosed {
		t.Fatalf("closed=%+v error=%v", closed, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WithArgs(int64(11)).WillReturnRows(bookingRow(11, 1, 1, 1, booking.StatusPendingPayment, now.Add(time.Minute), now))
	mock.ExpectExec("UPDATE bookings SET status = 'CANCELLED'").WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectCommit()
	cancelled, err := store.CancelPending(ctx, 1, 11)
	if err != nil || cancelled.Status != booking.StatusCancelled {
		t.Fatalf("cancelled=%+v error=%v", cancelled, err)
	}
}

func TestPaymentRepositoriesWithoutMySQL(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	store, mock := mockStore(t)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, booking_id, owner_id, amount_cents, status, created_at, updated_at").WithArgs(int64(10)).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT b.status, b.owner_id, b.expires_at, p.price_cents").WithArgs(int64(10)).WillReturnRows(sqlmock.NewRows([]string{"status", "owner_id", "expires_at", "price_cents"}).AddRow("PENDING_PAYMENT", 1, now.Add(5*time.Minute), 9900))
	mock.ExpectExec("INSERT INTO payments").WithArgs(int64(10), int64(1), int64(9900)).WillReturnResult(sqlmock.NewResult(20, 1))
	mock.ExpectQuery("SELECT created_at, updated_at FROM payments").WithArgs(int64(20)).WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))
	expectAudit(mock)
	mock.ExpectCommit()
	created, err := store.CreatePayment(ctx, 1, 10, now)
	if err != nil || created.ID != 20 || created.Status != payment.StatusPending {
		t.Fatalf("created=%+v error=%v", created, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT p.id, p.booking_id, p.owner_id, p.amount_cents, p.status").WithArgs(int64(20)).WillReturnRows(sqlmock.NewRows([]string{"id", "booking_id", "owner_id", "amount_cents", "status", "created_at", "updated_at", "booking_status", "expires_at"}).AddRow(20, 10, 1, 9900, "PENDING", now, now, "PENDING_PAYMENT", now.Add(5*time.Minute)))
	mock.ExpectExec("UPDATE payments SET status").WithArgs(payment.StatusSuccess, int64(20)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectExec("UPDATE bookings SET status = 'CONFIRMED'").WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectCommit()
	paid, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 20, BookingID: 10, AmountCents: 9900, Outcome: payment.OutcomeSuccess}, now)
	if err != nil || paid.Status != payment.StatusSuccess {
		t.Fatalf("paid=%+v error=%v", paid, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT p.id, p.booking_id, p.owner_id, p.amount_cents, p.status").WithArgs(int64(21)).WillReturnRows(sqlmock.NewRows([]string{"id", "booking_id", "owner_id", "amount_cents", "status", "created_at", "updated_at", "booking_status", "expires_at"}).AddRow(21, 11, 1, 9900, "PENDING", now, now, "PENDING_PAYMENT", now))
	mock.ExpectExec("UPDATE payments SET status").WithArgs(payment.StatusSuccess, int64(21)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectExec("UPDATE bookings SET status = 'CLOSED'").WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectExec("INSERT INTO exceptions").WithArgs(int64(21), int64(11), int64(9900)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO refunds").WithArgs(int64(11), int64(21), int64(9900)).WillReturnResult(sqlmock.NewResult(30, 1))
	expectAudit(mock)
	mock.ExpectCommit()
	late, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 21, BookingID: 11, AmountCents: 9900, Outcome: payment.OutcomeSuccess}, now)
	if err != nil || late.Status != payment.StatusSuccess {
		t.Fatalf("late=%+v error=%v", late, err)
	}
}

func TestCancellationRepositoriesWithoutMySQL(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	store, mock := mockStore(t)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WithArgs(int64(10)).WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
	mock.ExpectQuery("SELECT start_at FROM slots").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"start_at"}).AddRow(now.Add(3 * time.Hour)))
	mock.ExpectQuery("SELECT id, amount_cents, status FROM payments").WithArgs(int64(10)).WillReturnRows(sqlmock.NewRows([]string{"id", "amount_cents", "status"}).AddRow(20, 9900, "SUCCESS"))
	mock.ExpectExec("UPDATE bookings SET status = 'CANCELLED'").WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO refunds").WithArgs(int64(10), int64(20), int64(9900)).WillReturnResult(sqlmock.NewResult(30, 1))
	expectAudit(mock)
	mock.ExpectCommit()
	cancelled, err := store.CancelConfirmed(ctx, 1, 10, now)
	if err != nil || cancelled.Status != booking.StatusCancelled {
		t.Fatalf("cancelled=%+v error=%v", cancelled, err)
	}

	mock.ExpectBegin()
	expectAudit(mock)
	mock.ExpectQuery("SELECT b.status, b.owner_id, s.start_at").WithArgs(int64(11)).WillReturnRows(sqlmock.NewRows([]string{"status", "owner_id", "start_at"}).AddRow("CONFIRMED", 1, now.Add(time.Hour)))
	mock.ExpectExec("UPDATE bookings SET status = 'CANCEL_REVIEW'").WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO cancellation_requests").WithArgs(int64(11), int64(1), "reason").WillReturnResult(sqlmock.NewResult(40, 1))
	mock.ExpectQuery("SELECT id, booking_id, owner_id, reason, status, reviewed_by, created_at").WithArgs(int64(40)).WillReturnRows(sqlmock.NewRows([]string{"id", "booking_id", "owner_id", "reason", "status", "reviewed_by", "created_at"}).AddRow(40, 11, 1, "reason", "PENDING", 0, now))
	mock.ExpectCommit()
	request, err := store.RequestCancellation(ctx, 1, 11, "reason", now)
	if err != nil || request.ID != 40 || request.Status != cancellation.StatusPending {
		t.Fatalf("request=%+v error=%v", request, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT role FROM accounts").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("support"))
	mock.ExpectQuery("SELECT booking_id, status FROM cancellation_requests").WithArgs(int64(40)).WillReturnRows(sqlmock.NewRows([]string{"booking_id", "status"}).AddRow(11, "PENDING"))
	mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WithArgs(int64(11)).WillReturnRows(bookingRow(11, 1, 1, 1, booking.StatusCancelReview, now, now))
	mock.ExpectQuery("SELECT id, amount_cents, status FROM payments").WithArgs(int64(11)).WillReturnRows(sqlmock.NewRows([]string{"id", "amount_cents", "status"}).AddRow(21, 9900, "SUCCESS"))
	mock.ExpectExec("UPDATE bookings SET status = 'CANCELLED'").WithArgs(int64(11)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO refunds").WithArgs(int64(11), int64(21), int64(9900)).WillReturnResult(sqlmock.NewResult(31, 1))
	mock.ExpectExec("UPDATE cancellation_requests SET status = 'APPROVED'").WithArgs(int64(7), now, int64(40)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectCommit()
	reviewed, err := store.ReviewCancellation(ctx, 7, 40, true, now)
	if err != nil || reviewed.Status != booking.StatusCancelled {
		t.Fatalf("reviewed=%+v error=%v", reviewed, err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, booking_id, payment_id, amount_cents, status, created_at, updated_at").WithArgs(int64(31)).WillReturnRows(sqlmock.NewRows([]string{"id", "booking_id", "payment_id", "amount_cents", "status", "created_at", "updated_at"}).AddRow(31, 11, 21, 9900, "PENDING", now, now))
	mock.ExpectExec("UPDATE refunds SET status").WithArgs(refund.StatusSuccess, int64(31)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectAudit(mock)
	mock.ExpectCommit()
	refunded, err := store.ApplyRefundResult(ctx, 31, true)
	if err != nil || refunded.Status != refund.StatusSuccess {
		t.Fatalf("refunded=%+v error=%v", refunded, err)
	}
}

func TestFulfillmentRepositoriesWithoutMySQL(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC)
	store, mock := mockStore(t)

	expectTransition := func(role string, actorID int64, from, to booking.Status, eventType, reason string, eventID int64, eventTime time.Time) {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WithArgs(actorID).WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow(role, 1))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WithArgs(int64(10)).WillReturnRows(bookingRow(10, 1, 1, 1, from, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"store_id", "start_at", "end_at"}).AddRow(1, now, now.Add(time.Hour)))
		mock.ExpectExec("UPDATE bookings SET status").WithArgs(to, int64(10), from).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO service_events").WithArgs(int64(10), actorID, eventType, reason, eventTime).WillReturnResult(sqlmock.NewResult(eventID, 1))
		expectAudit(mock)
		mock.ExpectCommit()
	}

	expectTransition("desk", 5, booking.StatusConfirmed, booking.StatusCheckedIn, "CHECK_IN", "", 50, now)
	checked, event, err := store.CheckIn(ctx, 5, 10, now)
	if err != nil || checked.Status != booking.StatusCheckedIn || event.ID != 50 {
		t.Fatalf("checked=%+v event=%+v error=%v", checked, event, err)
	}

	expectTransition("worker", 6, booking.StatusCheckedIn, booking.StatusInService, "SERVICE_START", "", 51, now.Add(time.Minute))
	started, _, err := store.StartService(ctx, 6, 10, now.Add(time.Minute))
	if err != nil || started.Status != booking.StatusInService {
		t.Fatalf("started=%+v error=%v", started, err)
	}

	expectTransition("manager", 8, booking.StatusConfirmed, booking.StatusNoShow, "NO_SHOW", "reason", 52, now.Add(2*time.Hour))
	marked, _, err := store.MarkNoShow(ctx, 8, 10, "reason", now.Add(2*time.Hour))
	if err != nil || marked.Status != booking.StatusNoShow {
		t.Fatalf("marked=%+v error=%v", marked, err)
	}
}

func TestBookingRepositoryUnitBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

	t.Run("slot missing", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id, start_at FROM slots").WithArgs(int64(1)).WillReturnError(sql.ErrNoRows)
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); !errors.Is(err, booking.ErrNotFound) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("started slot", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id, start_at FROM slots").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"store_id", "start_at"}).AddRow(1, now))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); !errors.Is(err, booking.ErrSlotStarted) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("foreign pet", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id, start_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store_id", "start_at"}).AddRow(1, now.Add(time.Hour)))
		mock.ExpectQuery("SELECT id FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery("SELECT owner_id FROM pets").WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(2))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); !errors.Is(err, booking.ErrForbidden) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("duplicate pet", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id, start_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store_id", "start_at"}).AddRow(1, now.Add(time.Hour)))
		mock.ExpectQuery("SELECT id FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery("SELECT owner_id FROM pets").WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(1))
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); !errors.Is(err, booking.ErrDuplicatePet) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("capacity full", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id, start_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store_id", "start_at"}).AddRow(1, now.Add(time.Hour)))
		mock.ExpectQuery("SELECT id FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectQuery("SELECT owner_id FROM pets").WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow(1))
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
		mock.ExpectQuery("SELECT capacity FROM slots").WillReturnRows(sqlmock.NewRows([]string{"capacity"}).AddRow(3))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); !errors.Is(err, booking.ErrCapacityFull) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("close states", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(1, 1, 1, 1, booking.StatusClosed, now, now))
		closed, err := store.CloseExpired(ctx, 1, now)
		if err != nil || closed.Status != booking.StatusClosed {
			t.Fatalf("closed=%+v error=%v", closed, err)
		}
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(2, 1, 1, 1, booking.StatusConfirmed, now, now))
		if _, err := store.CloseExpired(ctx, 2, now); !errors.Is(err, booking.ErrInvalidState) {
			t.Fatalf("error=%v", err)
		}
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(3, 1, 1, 1, booking.StatusPendingPayment, now.Add(time.Minute), now))
		if _, err := store.CloseExpired(ctx, 3, now); !errors.Is(err, booking.ErrNotExpired) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("cancel states", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(1, 1, 1, 2, booking.StatusPendingPayment, now, now))
		if _, err := store.CancelPending(ctx, 1, 1); !errors.Is(err, booking.ErrForbidden) {
			t.Fatalf("error=%v", err)
		}
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(2, 1, 1, 1, booking.StatusCancelled, now, now))
		cancelled, err := store.CancelPending(ctx, 1, 2)
		if err != nil || cancelled.Status != booking.StatusCancelled {
			t.Fatalf("cancelled=%+v error=%v", cancelled, err)
		}
	})
}

func TestPaymentRepositoryUnitBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

	t.Run("existing payment", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, booking_id, owner_id, amount_cents, status, created_at, updated_at").WillReturnRows(sqlmock.NewRows([]string{"id", "booking_id", "owner_id", "amount_cents", "status", "created_at", "updated_at"}).AddRow(20, 10, 1, 9900, "PENDING", now, now))
		got, err := store.CreatePayment(ctx, 1, 10, now)
		if err != nil || got.ID != 20 {
			t.Fatalf("payment=%+v error=%v", got, err)
		}
	})

	t.Run("expired before create", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, booking_id, owner_id, amount_cents, status, created_at, updated_at").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT b.status, b.owner_id, b.expires_at, p.price_cents").WillReturnRows(sqlmock.NewRows([]string{"status", "owner_id", "expires_at", "price"}).AddRow("PENDING_PAYMENT", 1, now, 9900))
		mock.ExpectExec("UPDATE bookings SET status = 'CLOSED'").WillReturnResult(sqlmock.NewResult(0, 1))
		expectAudit(mock)
		mock.ExpectCommit()
		if _, err := store.CreatePayment(ctx, 1, 10, now); !errors.Is(err, payment.ErrBookingNotPayable) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("invalid callback facts", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			callback payment.Callback
			row      *sqlmock.Rows
			want     error
		}{
			{"booking", payment.Callback{PaymentID: 20, BookingID: 99, AmountCents: 9900}, sqlmock.NewRows([]string{"id", "booking_id", "owner_id", "amount", "status", "created", "updated", "booking_status", "expires"}).AddRow(20, 10, 1, 9900, "PENDING", now, now, "PENDING_PAYMENT", now.Add(time.Minute)), payment.ErrInvalidCallback},
			{"amount", payment.Callback{PaymentID: 20, BookingID: 10, AmountCents: 1}, sqlmock.NewRows([]string{"id", "booking_id", "owner_id", "amount", "status", "created", "updated", "booking_status", "expires"}).AddRow(20, 10, 1, 9900, "PENDING", now, now, "PENDING_PAYMENT", now.Add(time.Minute)), payment.ErrPaymentAmountMismatch},
		} {
			t.Run(tc.name, func(t *testing.T) {
				store, mock := mockStore(t)
				mock.ExpectBegin()
				mock.ExpectQuery("SELECT p.id, p.booking_id, p.owner_id, p.amount_cents, p.status").WillReturnRows(tc.row)
				if _, err := store.ApplyCallback(ctx, tc.callback, now); !errors.Is(err, tc.want) {
					t.Fatalf("error=%v", err)
				}
			})
		}
	})

	t.Run("failed and timeout callbacks", func(t *testing.T) {
		for i, outcome := range []string{payment.OutcomeFailed, payment.OutcomeTimeout} {
			store, mock := mockStore(t)
			id := int64(30 + i)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT p.id, p.booking_id, p.owner_id, p.amount_cents, p.status").WillReturnRows(sqlmock.NewRows([]string{"id", "booking_id", "owner_id", "amount", "status", "created", "updated", "booking_status", "expires"}).AddRow(id, 10, 1, 9900, "PENDING", now, now, "PENDING_PAYMENT", now.Add(time.Minute)))
			want := payment.StatusFailed
			if outcome == payment.OutcomeTimeout {
				want = payment.StatusTimeout
			}
			mock.ExpectExec("UPDATE payments SET status").WithArgs(want, id).WillReturnResult(sqlmock.NewResult(0, 1))
			expectAudit(mock)
			mock.ExpectCommit()
			got, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: id, BookingID: 10, AmountCents: 9900, Outcome: outcome}, now)
			if err != nil || got.Status != want {
				t.Fatalf("payment=%+v error=%v", got, err)
			}
		}
	})
}

func TestCancellationRepositoryUnitBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

	t.Run("cancel too late", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		mock.ExpectQuery("SELECT start_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"start_at"}).AddRow(now.Add(time.Hour)))
		if _, err := store.CancelConfirmed(ctx, 1, 10, now); !errors.Is(err, cancellation.ErrTooLate) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("request validation", func(t *testing.T) {
		store, _ := mockStore(t)
		if _, err := store.RequestCancellation(ctx, 1, 10, " ", now); !errors.Is(err, cancellation.ErrInvalidState) {
			t.Fatalf("empty reason error=%v", err)
		}

		store, mock := mockStore(t)
		mock.ExpectBegin()
		expectAudit(mock)
		mock.ExpectQuery("SELECT b.status, b.owner_id, s.start_at").WillReturnRows(sqlmock.NewRows([]string{"status", "owner_id", "start_at"}).AddRow("CONFIRMED", 2, now.Add(time.Hour)))
		if _, err := store.RequestCancellation(ctx, 1, 10, "reason", now); !errors.Is(err, cancellation.ErrForbidden) {
			t.Fatalf("foreign owner error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		expectAudit(mock)
		mock.ExpectQuery("SELECT b.status, b.owner_id, s.start_at").WillReturnRows(sqlmock.NewRows([]string{"status", "owner_id", "start_at"}).AddRow("CHECKED_IN", 1, now.Add(time.Hour)))
		if _, err := store.RequestCancellation(ctx, 1, 10, "reason", now); !errors.Is(err, cancellation.ErrInvalidState) {
			t.Fatalf("state error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		expectAudit(mock)
		mock.ExpectQuery("SELECT b.status, b.owner_id, s.start_at").WillReturnRows(sqlmock.NewRows([]string{"status", "owner_id", "start_at"}).AddRow("CONFIRMED", 1, now.Add(3*time.Hour)))
		if _, err := store.RequestCancellation(ctx, 1, 10, "reason", now); !errors.Is(err, cancellation.ErrTooEarly) {
			t.Fatalf("early error=%v", err)
		}
	})

	t.Run("review rejection", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("support"))
		mock.ExpectQuery("SELECT booking_id, status FROM cancellation_requests").WillReturnRows(sqlmock.NewRows([]string{"booking_id", "status"}).AddRow(10, "PENDING"))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusCancelReview, now, now))
		mock.ExpectExec("UPDATE bookings SET status = 'CONFIRMED'").WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("UPDATE cancellation_requests SET status = 'REJECTED'").WithArgs(int64(7), now, int64(40)).WillReturnResult(sqlmock.NewResult(0, 1))
		expectAudit(mock)
		mock.ExpectCommit()
		got, err := store.ReviewCancellation(ctx, 7, 40, false, now)
		if err != nil || got.Status != booking.StatusConfirmed {
			t.Fatalf("booking=%+v error=%v", got, err)
		}
	})

	t.Run("review authorization and state", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))
		if _, err := store.ReviewCancellation(ctx, 1, 40, true, now); !errors.Is(err, cancellation.ErrForbidden) {
			t.Fatalf("role error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("support"))
		mock.ExpectQuery("SELECT booking_id, status FROM cancellation_requests").WillReturnError(sql.ErrNoRows)
		if _, err := store.ReviewCancellation(ctx, 7, 40, true, now); !errors.Is(err, booking.ErrNotFound) {
			t.Fatalf("missing error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("support"))
		mock.ExpectQuery("SELECT booking_id, status FROM cancellation_requests").WillReturnRows(sqlmock.NewRows([]string{"booking_id", "status"}).AddRow(10, "REJECTED"))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		if _, err := store.ReviewCancellation(ctx, 7, 40, true, now); !errors.Is(err, cancellation.ErrInvalidState) {
			t.Fatalf("state error=%v", err)
		}
	})

	t.Run("refund states", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, booking_id, payment_id, amount_cents, status, created_at, updated_at").WillReturnRows(sqlmock.NewRows([]string{"id", "booking_id", "payment_id", "amount", "status", "created", "updated"}).AddRow(30, 10, 20, 9900, "SUCCESS", now, now))
		got, err := store.ApplyRefundResult(ctx, 30, true)
		if err != nil || got.Status != refund.StatusSuccess {
			t.Fatalf("refund=%+v error=%v", got, err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, booking_id, payment_id, amount_cents, status, created_at, updated_at").WillReturnRows(sqlmock.NewRows([]string{"id", "booking_id", "payment_id", "amount", "status", "created", "updated"}).AddRow(31, 10, 20, 9900, "PENDING", now, now))
		mock.ExpectExec("UPDATE refunds SET status").WithArgs(refund.StatusFailed, int64(31)).WillReturnResult(sqlmock.NewResult(0, 1))
		expectAudit(mock)
		mock.ExpectCommit()
		failed, err := store.ApplyRefundResult(ctx, 31, false)
		if err != nil || failed.Status != refund.StatusFailed {
			t.Fatalf("refund=%+v error=%v", failed, err)
		}
	})
}

func TestFulfillmentRepositoryUnitBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC)

	t.Run("check-in boundaries", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("desk", 2))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store_id", "start", "end"}).AddRow(1, now, now.Add(time.Hour)))
		if _, _, err := store.CheckIn(ctx, 5, 10, now); !errors.Is(err, fulfillment.ErrForbidden) {
			t.Fatalf("store error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("desk", 1))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store_id", "start", "end"}).AddRow(1, now.Add(time.Hour), now.Add(2*time.Hour)))
		if _, _, err := store.CheckIn(ctx, 5, 10, now); !errors.Is(err, fulfillment.ErrOutsideCheckInWindow) {
			t.Fatalf("window error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("desk", 1))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusCancelReview, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store_id", "start", "end"}).AddRow(1, now, now.Add(time.Hour)))
		if _, _, err := store.CheckIn(ctx, 5, 10, now); !errors.Is(err, fulfillment.ErrInvalidState) {
			t.Fatalf("state error=%v", err)
		}
	})

	t.Run("service boundaries", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("worker", 1))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusCheckedIn, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store", "start", "end"}).AddRow(1, now.Add(-time.Hour), now.Add(-time.Minute)))
		if _, _, err := store.StartService(ctx, 6, 10, now); !errors.Is(err, fulfillment.ErrInvalidState) {
			t.Fatalf("late start error=%v", err)
		}
	})

	t.Run("no-show validation", func(t *testing.T) {
		store, _ := mockStore(t)
		if _, _, err := store.MarkNoShow(ctx, 8, 10, " ", now); !errors.Is(err, fulfillment.ErrReasonRequired) {
			t.Fatalf("reason error=%v", err)
		}

		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("manager", 1))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store", "start", "end"}).AddRow(1, now, now.Add(time.Hour)))
		if _, _, err := store.MarkNoShow(ctx, 8, 10, "reason", now); !errors.Is(err, fulfillment.ErrTooEarly) {
			t.Fatalf("early error=%v", err)
		}
	})

	t.Run("role rejected", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("owner", 1))
		if _, _, err := store.CheckIn(ctx, 1, 10, now); !errors.Is(err, fulfillment.ErrForbidden) {
			t.Fatalf("role error=%v", err)
		}
	})
}

func TestRepositoryHelperUnitBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

	t.Run("booking locks", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnError(sql.ErrNoRows)
		tx, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lockBooking(ctx, tx, 10); !errors.Is(err, booking.ErrNotFound) {
			t.Fatalf("lock error=%v", err)
		}
		_ = tx.Rollback()

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 2, booking.StatusConfirmed, now, now))
		tx, _ = store.db.BeginTx(ctx, nil)
		if _, _, err := lockConfirmedBooking(ctx, tx, 1, 10); !errors.Is(err, cancellation.ErrForbidden) {
			t.Fatalf("owner error=%v", err)
		}
		_ = tx.Rollback()

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusPendingPayment, now, now))
		tx, _ = store.db.BeginTx(ctx, nil)
		if _, _, err := lockConfirmedBooking(ctx, tx, 1, 10); !errors.Is(err, cancellation.ErrInvalidState) {
			t.Fatalf("state error=%v", err)
		}
		_ = tx.Rollback()
	})

	t.Run("payment lookup", func(t *testing.T) {
		for name, tc := range map[string]struct {
			status string
			want   error
		}{
			"missing": {"", payment.ErrBookingNotPayable},
			"failed":  {"FAILED", payment.ErrBookingNotPayable},
		} {
			t.Run(name, func(t *testing.T) {
				store, mock := mockStore(t)
				mock.ExpectBegin()
				query := mock.ExpectQuery("SELECT id, amount_cents, status FROM payments")
				if tc.status == "" {
					query.WillReturnError(sql.ErrNoRows)
				} else {
					query.WillReturnRows(sqlmock.NewRows([]string{"id", "amount", "status"}).AddRow(20, 9900, tc.status))
				}
				tx, _ := store.db.BeginTx(ctx, nil)
				if _, _, err := successfulPayment(ctx, tx, 10); !errors.Is(err, tc.want) {
					t.Fatalf("error=%v", err)
				}
				_ = tx.Rollback()
			})
		}
	})

	t.Run("roles and slot lock", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnError(sql.ErrNoRows)
		tx, _ := store.db.BeginTx(ctx, nil)
		if _, err := requireRole(ctx, tx, 99, "desk"); !errors.Is(err, fulfillment.ErrForbidden) {
			t.Fatalf("missing role error=%v", err)
		}
		_ = tx.Rollback()

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("owner", 1))
		tx, _ = store.db.BeginTx(ctx, nil)
		if _, err := requireRole(ctx, tx, 1, "desk"); !errors.Is(err, fulfillment.ErrForbidden) {
			t.Fatalf("wrong role error=%v", err)
		}
		_ = tx.Rollback()

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnError(sql.ErrNoRows)
		tx, _ = store.db.BeginTx(ctx, nil)
		if _, _, _, _, err := lockBookingSlot(ctx, tx, 10); !errors.Is(err, fulfillment.ErrNotFound) {
			t.Fatalf("booking slot error=%v", err)
		}
		_ = tx.Rollback()
	})

	t.Run("transition rejected", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE bookings SET status").WillReturnResult(sqlmock.NewResult(0, 0))
		tx, _ := store.db.BeginTx(ctx, nil)
		b := booking.Booking{ID: 10, Status: booking.StatusConfirmed}
		if _, _, err := recordTransition(ctx, tx, b, 5, fulfillment.EventCheckIn, "", booking.StatusCheckedIn, now); !errors.Is(err, fulfillment.ErrInvalidState) {
			t.Fatalf("transition error=%v", err)
		}
		_ = tx.Rollback()

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE bookings SET status").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO service_events").WillReturnError(errors.New("event failed"))
		tx, _ = store.db.BeginTx(ctx, nil)
		if _, _, err := recordTransition(ctx, tx, b, 5, fulfillment.EventCheckIn, "", booking.StatusCheckedIn, now); err == nil {
			t.Fatal("event error was ignored")
		}
		_ = tx.Rollback()
	})
}

func TestSeedAndReadinessUnitBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

	for failAt := range 17 {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		for i := 0; i < failAt; i++ {
			mock.ExpectExec("(?s).+").WillReturnResult(sqlmock.NewResult(1, 1))
		}
		mock.ExpectExec("(?s).+").WillReturnError(errors.New("seed failed"))
		if err := store.SeedTeachingData(ctx, now); err == nil {
			t.Fatalf("seed step %d error was ignored", failAt)
		}
	}

	t.Run("password account missing", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM accounts").WillReturnError(sql.ErrNoRows)
		if err := store.ProvisionPassword(ctx, "missing", []byte("password")); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("readiness incomplete", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectQuery("SELECT id, username, password_hash, role, store_id, owner_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"id", "username", "hash", "role", "store", "owner"}))
		if _, err := store.UnconfiguredAccounts(ctx); err == nil {
			t.Fatal("incomplete accounts accepted")
		}
	})

	t.Run("slot authorization", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id FROM projects").WillReturnError(sql.ErrNoRows)
		if _, err := store.Create(ctx, 8, 1, 1, now, now.Add(time.Hour), 3); !errors.Is(err, slot.ErrNotFound) {
			t.Fatalf("missing project error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id FROM projects").WillReturnRows(sqlmock.NewRows([]string{"store_id"}).AddRow(2))
		if _, err := store.Create(ctx, 8, 1, 1, now, now.Add(time.Hour), 3); !errors.Is(err, slot.ErrForbidden) {
			t.Fatalf("foreign project error=%v", err)
		}
	})
}

func TestAuthenticationAndReadinessErrorBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("token"))

	t.Run("not found", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectQuery("SELECT id, username, password_hash").WillReturnError(sql.ErrNoRows)
		if _, err := store.AccountByUsername(ctx, "missing"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("account error=%v", err)
		}
		mock.ExpectQuery("SELECT a.id, a.username, a.password_hash").WillReturnError(sql.ErrNoRows)
		if _, err := store.SessionByDigest(ctx, digest); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("session error=%v", err)
		}
	})

	t.Run("session password changed", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT password_hash FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow("new-hash"))
		if err := store.SaveSession(ctx, digest, 1, "old-hash", now); !errors.Is(err, auth.ErrUnauthorized) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("password writes", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectExec("UPDATE accounts SET password_hash").WillReturnError(errors.New("update failed"))
		if err := store.ProvisionPassword(ctx, "owner1", []byte("password")); err == nil {
			t.Fatal("update error ignored")
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectExec("UPDATE accounts SET password_hash").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("DELETE FROM sessions").WillReturnError(errors.New("delete failed"))
		if err := store.ProvisionPassword(ctx, "owner1", []byte("password")); err == nil {
			t.Fatal("session deletion error ignored")
		}
	})

	t.Run("invalid account rows", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectQuery("SELECT id, username, password_hash, role, store_id, owner_id FROM accounts").WillReturnRows(
			sqlmock.NewRows([]string{"id", "username", "hash", "role", "store", "owner"}).AddRow(99, "wrong", "", "owner", 1, 1),
		)
		if _, err := store.UnconfiguredAccounts(ctx); err == nil {
			t.Fatal("unexpected identity accepted")
		}

		store, mock = mockStore(t)
		rows := sqlmock.NewRows([]string{"id", "username", "hash", "role", "store", "owner"})
		for _, item := range teachingAccounts {
			rows.AddRow(item.id, item.name, "not-bcrypt", item.role, 1, item.ownerID)
		}
		mock.ExpectQuery("SELECT id, username, password_hash, role, store_id, owner_id FROM accounts").WillReturnRows(rows)
		if _, err := store.UnconfiguredAccounts(ctx); err == nil {
			t.Fatal("invalid password hash accepted")
		}
	})
}

func TestPaymentAndStateErrorBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

	t.Run("payment creation state", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, booking_id, owner_id, amount_cents, status, created_at, updated_at").WillReturnRows(sqlmock.NewRows([]string{"id", "booking", "owner", "amount", "status", "created", "updated"}).AddRow(20, 10, 2, 9900, "PENDING", now, now))
		if _, err := store.CreatePayment(ctx, 1, 10, now); !errors.Is(err, booking.ErrForbidden) {
			t.Fatalf("existing owner error=%v", err)
		}

		for name, tc := range map[string]struct {
			row  *sqlmock.Rows
			want error
		}{
			"missing": {nil, booking.ErrNotFound},
			"foreign": {sqlmock.NewRows([]string{"status", "owner", "expires", "price"}).AddRow("PENDING_PAYMENT", 2, now.Add(time.Minute), 9900), booking.ErrForbidden},
			"state":   {sqlmock.NewRows([]string{"status", "owner", "expires", "price"}).AddRow("CONFIRMED", 1, now.Add(time.Minute), 9900), payment.ErrBookingNotPayable},
		} {
			t.Run(name, func(t *testing.T) {
				store, mock := mockStore(t)
				mock.ExpectBegin()
				mock.ExpectQuery("SELECT id, booking_id, owner_id, amount_cents, status, created_at, updated_at").WillReturnError(sql.ErrNoRows)
				q := mock.ExpectQuery("SELECT b.status, b.owner_id, b.expires_at, p.price_cents")
				if tc.row == nil {
					q.WillReturnError(sql.ErrNoRows)
				} else {
					q.WillReturnRows(tc.row)
				}
				if _, err := store.CreatePayment(ctx, 1, 10, now); !errors.Is(err, tc.want) {
					t.Fatalf("error=%v", err)
				}
			})
		}
	})

	t.Run("callback missing and terminal", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT p.id, p.booking_id, p.owner_id, p.amount_cents, p.status").WillReturnError(sql.ErrNoRows)
		if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 20}, now); !errors.Is(err, payment.ErrPaymentNotFound) {
			t.Fatalf("missing error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT p.id, p.booking_id, p.owner_id, p.amount_cents, p.status").WillReturnRows(sqlmock.NewRows([]string{"id", "booking", "owner", "amount", "status", "created", "updated", "booking_status", "expires"}).AddRow(20, 10, 1, 9900, "SUCCESS", now, now, "CONFIRMED", now))
		got, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 20, BookingID: 10, AmountCents: 9900}, now)
		if err != nil || got.Status != payment.StatusSuccess {
			t.Fatalf("payment=%+v error=%v", got, err)
		}
	})

	t.Run("cancellation owner and payment", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 2, booking.StatusConfirmed, now, now))
		if _, err := store.CancelConfirmed(ctx, 1, 10, now); !errors.Is(err, cancellation.ErrForbidden) {
			t.Fatalf("owner error=%v", err)
		}

		store, mock = mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		mock.ExpectQuery("SELECT start_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"start_at"}).AddRow(now.Add(3 * time.Hour)))
		mock.ExpectQuery("SELECT id, amount_cents, status FROM payments").WillReturnRows(sqlmock.NewRows([]string{"id", "amount", "status"}).AddRow(20, 9900, "FAILED"))
		if _, err := store.CancelConfirmed(ctx, 1, 10, now); !errors.Is(err, payment.ErrBookingNotPayable) {
			t.Fatalf("payment error=%v", err)
		}
	})
}

func TestTransactionFailureUnitBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

	t.Run("booking insert", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCreatePendingPrefix(mock, now)
		mock.ExpectExec("INSERT INTO bookings").WillReturnError(errors.New("insert failed"))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); err == nil {
			t.Fatal("insert error ignored")
		}
	})

	t.Run("booking insert id", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCreatePendingPrefix(mock, now)
		mock.ExpectExec("INSERT INTO bookings").WillReturnResult(sqlmock.NewErrorResult(errors.New("id failed")))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); err == nil {
			t.Fatal("last insert id error ignored")
		}
	})

	t.Run("booking timestamp", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCreatePendingPrefix(mock, now)
		mock.ExpectExec("INSERT INTO bookings").WillReturnResult(sqlmock.NewResult(10, 1))
		mock.ExpectQuery("SELECT created_at, updated_at FROM bookings").WillReturnError(errors.New("timestamp failed"))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); err == nil {
			t.Fatal("timestamp error ignored")
		}
	})

	t.Run("booking audit", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCreatePendingPrefix(mock, now)
		mock.ExpectExec("INSERT INTO bookings").WillReturnResult(sqlmock.NewResult(10, 1))
		mock.ExpectQuery("SELECT created_at, updated_at FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"created", "updated"}).AddRow(now, now))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(errors.New("audit failed"))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); err == nil {
			t.Fatal("audit error ignored")
		}
	})

	t.Run("booking commit", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCreatePendingPrefix(mock, now)
		mock.ExpectExec("INSERT INTO bookings").WillReturnResult(sqlmock.NewResult(10, 1))
		mock.ExpectQuery("SELECT created_at, updated_at FROM bookings").WillReturnRows(sqlmock.NewRows([]string{"created", "updated"}).AddRow(now, now))
		expectAudit(mock)
		mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
		if _, err := store.CreatePending(ctx, 1, 1, 1, now); err == nil {
			t.Fatal("commit error ignored")
		}
	})

	t.Run("payment update", func(t *testing.T) {
		store, mock := mockStore(t)
		expectPaymentCallbackPrefix(mock, now)
		mock.ExpectExec("UPDATE payments SET status").WillReturnError(errors.New("update failed"))
		if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 20, BookingID: 10, AmountCents: 9900, Outcome: payment.OutcomeSuccess}, now); err == nil {
			t.Fatal("payment update error ignored")
		}
	})

	t.Run("payment audit", func(t *testing.T) {
		store, mock := mockStore(t)
		expectPaymentCallbackPrefix(mock, now)
		mock.ExpectExec("UPDATE payments SET status").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(errors.New("audit failed"))
		if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 20, BookingID: 10, AmountCents: 9900, Outcome: payment.OutcomeSuccess}, now); err == nil {
			t.Fatal("payment audit error ignored")
		}
	})

	t.Run("booking confirmation", func(t *testing.T) {
		store, mock := mockStore(t)
		expectPaymentCallbackPrefix(mock, now)
		mock.ExpectExec("UPDATE payments SET status").WillReturnResult(sqlmock.NewResult(0, 1))
		expectAudit(mock)
		mock.ExpectExec("UPDATE bookings SET status = 'CONFIRMED'").WillReturnError(errors.New("confirm failed"))
		if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 20, BookingID: 10, AmountCents: 9900, Outcome: payment.OutcomeSuccess}, now); err == nil {
			t.Fatal("booking confirmation error ignored")
		}
	})

	t.Run("payment commit", func(t *testing.T) {
		store, mock := mockStore(t)
		expectPaymentCallbackPrefix(mock, now)
		mock.ExpectExec("UPDATE payments SET status").WillReturnResult(sqlmock.NewResult(0, 1))
		expectAudit(mock)
		mock.ExpectExec("UPDATE bookings SET status = 'CONFIRMED'").WillReturnResult(sqlmock.NewResult(0, 1))
		expectAudit(mock)
		mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
		if _, err := store.ApplyCallback(ctx, payment.Callback{PaymentID: 20, BookingID: 10, AmountCents: 9900, Outcome: payment.OutcomeSuccess}, now); err == nil {
			t.Fatal("payment commit error ignored")
		}
	})

	t.Run("slot sequence", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT store_id FROM projects").WillReturnRows(sqlmock.NewRows([]string{"store_id"}).AddRow(1))
		mock.ExpectQuery("SELECT next_id FROM slot_sequence").WillReturnError(errors.New("sequence failed"))
		if _, err := store.Create(ctx, 8, 1, 1, now, now.Add(time.Hour), 3); err == nil {
			t.Fatal("sequence error ignored")
		}
	})

	t.Run("slot sequence update", func(t *testing.T) {
		store, mock := mockStore(t)
		expectSlotCreatePrefix(mock, now)
		mock.ExpectExec("UPDATE slot_sequence").WillReturnError(errors.New("update failed"))
		if _, err := store.Create(ctx, 8, 1, 1, now, now.Add(time.Hour), 3); err == nil {
			t.Fatal("sequence update error ignored")
		}
	})

	t.Run("slot insert", func(t *testing.T) {
		store, mock := mockStore(t)
		expectSlotCreatePrefix(mock, now)
		mock.ExpectExec("UPDATE slot_sequence").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO slots").WillReturnError(errors.New("insert failed"))
		if _, err := store.Create(ctx, 8, 1, 1, now, now.Add(time.Hour), 3); err == nil {
			t.Fatal("slot insert error ignored")
		}
	})

	t.Run("slot audit", func(t *testing.T) {
		store, mock := mockStore(t)
		expectSlotCreatePrefix(mock, now)
		mock.ExpectExec("UPDATE slot_sequence").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO slots").WillReturnResult(sqlmock.NewResult(4, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(errors.New("audit failed"))
		if _, err := store.Create(ctx, 8, 1, 1, now, now.Add(time.Hour), 3); err == nil {
			t.Fatal("slot audit error ignored")
		}
	})

	t.Run("refund update", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, booking_id, payment_id, amount_cents, status, created_at, updated_at").WillReturnRows(sqlmock.NewRows([]string{"id", "booking", "payment", "amount", "status", "created", "updated"}).AddRow(30, 10, 20, 9900, "PENDING", now, now))
		mock.ExpectExec("UPDATE refunds SET status").WillReturnError(errors.New("update failed"))
		if _, err := store.ApplyRefundResult(ctx, 30, true); err == nil {
			t.Fatal("refund update error ignored")
		}
	})

	t.Run("refund audit", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, booking_id, payment_id, amount_cents, status, created_at, updated_at").WillReturnRows(sqlmock.NewRows([]string{"id", "booking", "payment", "amount", "status", "created", "updated"}).AddRow(30, 10, 20, 9900, "PENDING", now, now))
		mock.ExpectExec("UPDATE refunds SET status").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(errors.New("audit failed"))
		if _, err := store.ApplyRefundResult(ctx, 30, true); err == nil {
			t.Fatal("refund audit error ignored")
		}
	})

	t.Run("transition affected rows", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE bookings SET status").WillReturnResult(sqlmock.NewErrorResult(errors.New("rows failed")))
		tx, _ := store.db.BeginTx(ctx, nil)
		_, _, err := recordTransition(ctx, tx, booking.Booking{ID: 10, Status: booking.StatusConfirmed}, 5, fulfillment.EventCheckIn, "", booking.StatusCheckedIn, now)
		_ = tx.Rollback()
		if err == nil {
			t.Fatal("rows affected error ignored")
		}
	})

	t.Run("transition audit", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE bookings SET status").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO service_events").WillReturnResult(sqlmock.NewResult(50, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(errors.New("audit failed"))
		tx, _ := store.db.BeginTx(ctx, nil)
		_, _, err := recordTransition(ctx, tx, booking.Booking{ID: 10, Status: booking.StatusConfirmed}, 5, fulfillment.EventCheckIn, "", booking.StatusCheckedIn, now)
		_ = tx.Rollback()
		if err == nil {
			t.Fatal("transition audit error ignored")
		}
	})
}

func TestRemainingStateUnitBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC)

	t.Run("cancel confirmed pending operation", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		if _, err := store.CancelPending(ctx, 1, 10); !errors.Is(err, booking.ErrInvalidState) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("close missing booking", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnError(sql.ErrNoRows)
		if _, err := store.CloseExpired(ctx, 99, now); !errors.Is(err, booking.ErrNotFound) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("service store mismatch", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("worker", 2))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusCheckedIn, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store", "start", "end"}).AddRow(1, now, now.Add(time.Hour)))
		if _, _, err := store.StartService(ctx, 6, 10, now); !errors.Is(err, fulfillment.ErrForbidden) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("service wrong state", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("worker", 1))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store", "start", "end"}).AddRow(1, now, now.Add(time.Hour)))
		if _, _, err := store.StartService(ctx, 6, 10, now); !errors.Is(err, fulfillment.ErrInvalidState) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("no-show wrong state", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("manager", 1))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusCheckedIn, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store", "start", "end"}).AddRow(1, now.Add(-2*time.Hour), now.Add(-time.Hour)))
		if _, _, err := store.MarkNoShow(ctx, 8, 10, "reason", now); !errors.Is(err, fulfillment.ErrInvalidState) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("no-show store mismatch", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT role, store_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"role", "store_id"}).AddRow("manager", 2))
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnRows(sqlmock.NewRows([]string{"store", "start", "end"}).AddRow(1, now.Add(-2*time.Hour), now.Add(-time.Hour)))
		if _, _, err := store.MarkNoShow(ctx, 8, 10, "reason", now); !errors.Is(err, fulfillment.ErrForbidden) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("refund terminal state", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, booking_id, payment_id, amount_cents, status, created_at, updated_at").WillReturnRows(sqlmock.NewRows([]string{"id", "booking", "payment", "amount", "status", "created", "updated"}).AddRow(30, 10, 20, 9900, "CLOSED", now, now))
		got, err := store.ApplyRefundResult(ctx, 30, true)
		if err != nil || string(got.Status) != "CLOSED" {
			t.Fatalf("refund=%+v error=%v", got, err)
		}
	})

	t.Run("slot lookup failed", func(t *testing.T) {
		store, mock := mockStore(t)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id, slot_id, pet_id, owner_id, status, expires_at").WillReturnRows(bookingRow(10, 1, 1, 1, booking.StatusConfirmed, now, now))
		mock.ExpectQuery("SELECT store_id, start_at, end_at FROM slots").WillReturnError(errors.New("slot failed"))
		tx, _ := store.db.BeginTx(ctx, nil)
		_, _, _, _, err := lockBookingSlot(ctx, tx, 10)
		_ = tx.Rollback()
		if err == nil {
			t.Fatal("slot error ignored")
		}
	})
}

func TestCancellationTransactionFailureBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

	t.Run("request update", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCancellationRequestPrefix(mock, now)
		mock.ExpectExec("UPDATE bookings SET status = 'CANCEL_REVIEW'").WillReturnError(errors.New("update failed"))
		if _, err := store.RequestCancellation(ctx, 1, 10, "reason", now); err == nil {
			t.Fatal("request update error ignored")
		}
	})

	t.Run("request insert", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCancellationRequestPrefix(mock, now)
		mock.ExpectExec("UPDATE bookings SET status = 'CANCEL_REVIEW'").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO cancellation_requests").WillReturnError(errors.New("insert failed"))
		if _, err := store.RequestCancellation(ctx, 1, 10, "reason", now); err == nil {
			t.Fatal("request insert error ignored")
		}
	})

	t.Run("request id", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCancellationRequestPrefix(mock, now)
		mock.ExpectExec("UPDATE bookings SET status = 'CANCEL_REVIEW'").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO cancellation_requests").WillReturnResult(sqlmock.NewErrorResult(errors.New("id failed")))
		if _, err := store.RequestCancellation(ctx, 1, 10, "reason", now); err == nil {
			t.Fatal("request id error ignored")
		}
	})

	t.Run("request reload", func(t *testing.T) {
		store, mock := mockStore(t)
		expectCancellationRequestPrefix(mock, now)
		mock.ExpectExec("UPDATE bookings SET status = 'CANCEL_REVIEW'").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO cancellation_requests").WillReturnResult(sqlmock.NewResult(40, 1))
		mock.ExpectQuery("SELECT id, booking_id, owner_id, reason, status, reviewed_by, created_at").WillReturnError(errors.New("reload failed"))
		if _, err := store.RequestCancellation(ctx, 1, 10, "reason", now); err == nil {
			t.Fatal("request reload error ignored")
		}
	})

	t.Run("review booking update", func(t *testing.T) {
		store, mock := mockStore(t)
		expectReviewPrefix(mock, now)
		mock.ExpectExec("UPDATE bookings SET status = 'CANCELLED'").WillReturnError(errors.New("update failed"))
		if _, err := store.ReviewCancellation(ctx, 7, 40, true, now); err == nil {
			t.Fatal("review booking update error ignored")
		}
	})

	t.Run("review refund", func(t *testing.T) {
		store, mock := mockStore(t)
		expectReviewPrefix(mock, now)
		mock.ExpectExec("UPDATE bookings SET status = 'CANCELLED'").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO refunds").WillReturnError(errors.New("refund failed"))
		if _, err := store.ReviewCancellation(ctx, 7, 40, true, now); err == nil {
			t.Fatal("review refund error ignored")
		}
	})

	t.Run("review request update", func(t *testing.T) {
		store, mock := mockStore(t)
		expectReviewPrefix(mock, now)
		mock.ExpectExec("UPDATE bookings SET status = 'CANCELLED'").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO refunds").WillReturnResult(sqlmock.NewResult(30, 1))
		mock.ExpectExec("UPDATE cancellation_requests SET status = 'APPROVED'").WillReturnError(errors.New("request failed"))
		if _, err := store.ReviewCancellation(ctx, 7, 40, true, now); err == nil {
			t.Fatal("review request update error ignored")
		}
	})

	t.Run("review audit", func(t *testing.T) {
		store, mock := mockStore(t)
		expectReviewPrefix(mock, now)
		mock.ExpectExec("UPDATE bookings SET status = 'CANCELLED'").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO refunds").WillReturnResult(sqlmock.NewResult(30, 1))
		mock.ExpectExec("UPDATE cancellation_requests SET status = 'APPROVED'").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO audit_logs").WillReturnError(errors.New("audit failed"))
		if _, err := store.ReviewCancellation(ctx, 7, 40, true, now); err == nil {
			t.Fatal("review audit error ignored")
		}
	})
}

func TestUnitRepositoryErrorBranches(t *testing.T) {
	store, mock := mockStore(t)
	mock.ExpectQuery("SELECT id, username, password_hash").WithArgs("missing").WillReturnError(errors.New("read failed"))
	if _, err := store.AccountByUsername(context.Background(), "missing"); err == nil {
		t.Fatal("account read error was ignored")
	}
	mock.ExpectQuery("SELECT offset_seconds").WillReturnRows(sqlmock.NewRows([]string{"offset_seconds"}).AddRow(-1))
	if _, err := store.ClockNow(context.Background(), time.Now()); err == nil {
		t.Fatal("negative clock offset accepted")
	}
	mock.ExpectExec("UPDATE app_clock").WithArgs(int64(60), maxClockOffsetSeconds-60).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.AdvanceClock(context.Background(), 1); err == nil {
		t.Fatal("overflowing clock advance accepted")
	}
	if err := store.ProvisionPassword(context.Background(), "owner1", nil); err == nil {
		t.Fatal("empty password accepted")
	}
}
