# Query and Daily Reconciliation Implementation Plan

> **For Codex:** Execute this plan task-by-task with test-driven development and verification before claiming completion.

**Goal:** Add durable role-scoped booking queries and an Asia/Shanghai daily reconciliation API for the topic 02 acceptance flow.

**Architecture:** Add a small reporting package whose repository reads the existing booking, slot, payment, cancellation, refund, and service-event tables. Owners see only their own bookings; staff roles see bookings from their session store; managers can request a natural-day summary using Shanghai day boundaries converted to UTC query parameters. HTTP remains session-authenticated and returns JSON with explicit role/error mapping.

**Tech Stack:** Go 1.26, `database/sql`, MySQL 8, existing `net/http` handlers and teaching clock.

---

### Task 1: Define reporting models and failing storage tests

**Files:**
- Create: `internal/reporting/model.go`
- Create: `internal/reporting/service.go`
- Create: `internal/store/mysql/reporting_integration_test.go`

**Steps:**
1. Define booking view and daily summary models plus repository/service interfaces.
2. Write integration tests for owner scoping, same-store staff scoping, Shanghai day boundaries, payment/refund totals, and status counts.
3. Run the focused tests and confirm they fail because the repository methods are absent.

### Task 2: Implement MySQL reporting queries

**Files:**
- Create: `internal/store/mysql/reporting.go`

**Steps:**
1. Implement role-scoped booking listing with slot, pet, payment, cancellation, refund, and service-event facts.
2. Implement daily summary aggregation using UTC boundaries supplied by the service, without relying on MySQL timezone tables.
3. Run the focused integration tests until green.

### Task 3: Add authenticated HTTP query endpoints

**Files:**
- Modify: `internal/httpapi/auth.go`
- Modify: `cmd/pet-appointment/main.go`
- Create: `internal/httpapi/reporting_test.go`

**Steps:**
1. Write failing HTTP tests for `/bookings`, `/daily-summary`, session scoping, role restrictions, and invalid dates.
2. Add reporting dependency injection and handlers.
3. Run HTTP and full regression tests.

### Task 4: Record evidence and coverage

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/dev06-progress.md`
- Modify: `docs/acceptance-dev06-progress.md`

**Steps:**
1. Run full tests, MySQL integration tests, `go vet`, build, prototype tests, Docker race tests, and package coverage commands.
2. Record what query/daily evidence now passes and keep any remaining page/HTTPS/deployment blockers explicit.
3. Run `git diff --check` and review the final scope.
