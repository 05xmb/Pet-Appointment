# Acceptance Findings Fix Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task.

**Goal:** Fix the four local acceptance findings without changing the confirmed business decisions or deployment scope.

**Architecture:** Keep booking state changes inside existing MySQL transactions. Expire overdue pending bookings while holding the relevant slot or booking lock, make payment success branch on both status and `expires_at`, and treat `COMPLETED` as consumed slot capacity. Add database-independent SQL repository tests with `go-sqlmock` so unit coverage is measured separately from real-MySQL integration coverage.

**Tech Stack:** Go 1.26, `database/sql`, MySQL 8, `go-sqlmock`, Node test runner, Docker race detector.

---

### Task 1: Enforce five-minute expiry at capacity calculation

**Files:**
- Modify: `internal/store/mysql/booking_integration_test.go`
- Modify: `internal/store/mysql/booking.go`

1. Add an integration test that fills a slot, advances past the five-minute expiry without calling `CloseExpired`, and verifies a new reservation succeeds while expired rows become `CLOSED` exactly once.
2. Run the targeted test and confirm it fails with `booking capacity is full`.
3. In the slot-locked booking transaction, close overdue `PENDING_PAYMENT` rows and audit each transition before counting occupied rows.
4. Re-run the targeted booking tests and confirm they pass.

### Task 2: Reject expired payment confirmation as a late payment

**Files:**
- Modify: `internal/store/mysql/payment_integration_test.go`
- Modify: `internal/store/mysql/payment.go`

1. Add a test that creates a payment before expiry, sends success after expiry without explicit closing, and expects `CLOSED`, one exception, and one pending refund.
2. Run it and confirm the current implementation incorrectly returns `CONFIRMED`.
3. Load `expires_at` under the payment/booking lock; when success arrives at or after expiry, close the pending booking, then use the existing late-payment exception/refund path.
4. Re-run payment and concurrency-adjacent integration tests.

### Task 3: Preserve capacity after service completion

**Files:**
- Modify: `internal/store/mysql/booking_integration_test.go`
- Modify: `internal/store/mysql/booking.go`

1. Add a test with three confirmed reservations, complete one before slot start, and verify a fourth reservation is still rejected.
2. Run it and confirm the fourth reservation currently succeeds.
3. Include `COMPLETED` in the occupied-capacity status set.
4. Re-run booking and fulfillment tests.

### Task 4: Roll back cancellation transactions on every error

**Files:**
- Modify: `internal/store/mysql/audit_integration_test.go`
- Modify: `internal/store/mysql/cancellation.go`

1. Add a regression test with a one-connection pool and a missing audit table; after `RequestCancellation` fails, verify a subsequent query obtains the connection instead of timing out.
2. Run it and confirm the connection is leaked.
3. Move `defer tx.Rollback()` immediately after successful `BeginTx`.
4. Re-run audit and cancellation tests.

### Task 5: Establish a genuine unit-coverage gate for the MySQL repository

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/store/mysql/store_unit_test.go`
- Modify: `docs/coverage-2026-11-13.md`

1. Add `go-sqlmock` as a test-only dependency.
2. Add database-independent tests for repository happy paths, validation branches, transaction rollback, authorization, callback idempotency, reporting, and persistence error propagation.
3. Run `go test -count=1 -coverprofile=<unit profile> ./...` without `PET_MYSQL_TEST_DSN`; confirm every executable self-written package and the combined profile meet 85%, with MySQL integration tests skipped.
4. Run the real-MySQL suite separately and retain it as integration evidence, not as unit coverage evidence.
5. Update the coverage report with separate unit and integration commands and actual results.

### Task 6: Full rule and acceptance regression

**Files:**
- Modify: `docs/skill-usage.md`
- Create: `docs/acceptance-fix-2026-11-14.md`

1. Run `gofmt`, unit tests and coverage, real-MySQL tests, `go vet`, build, prototype tests, and Docker `-race`.
2. Re-run the `pet-appointment-rules` checklist for capacity, expiry, late payment, cancellation rollback, and daily reconciliation.
3. Re-run the `pet-appointment-acceptance` local non-deployment checklist against the exact branch commit/worktree.
4. Record commands, outputs, remaining external/deployment items, and keep any unverified claim marked as such.
