# DEV05 Fulfillment Implementation Plan

> **For Codex:** Execute this plan task-by-task with test-driven development and verification before claiming completion.

**Goal:** Implement the topic 02 check-in, service fulfillment, no-show, and cancellation competition rules as durable MySQL state transitions.

**Architecture:** Keep booking as the authoritative lifecycle state, use row locks plus conditional updates for state transitions, and add a separate service event record for start/completion evidence. Check-in is restricted to the booking's store and D04's time window; cancellation review and no-show compete through locked state transitions so only one operation succeeds.

**Tech Stack:** Go 1.26, `database/sql`, MySQL 8, existing booking/cancellation services and integration-test fixtures.

---

### Task 1: Define fulfillment records and errors

**Files:**
- Modify: `internal/booking/model.go`
- Create: `internal/fulfillment/model.go`
- Create: `internal/fulfillment/service.go`
- Test: `internal/fulfillment/service_test.go`

**Steps:**
1. Add explicit errors and repository interfaces for check-in, service start, service completion, and no-show.
2. Add a service event model that records check-in/start/completion/no-show facts without replacing the booking lifecycle state.
3. Write unit tests for allowed state transitions and invalid transitions.
4. Run the focused unit tests and confirm they fail before storage implementation.

### Task 2: Add MySQL schema and check-in CAS

**Files:**
- Modify: `internal/store/mysql/auth.go`
- Create: `internal/store/mysql/fulfillment.go`
- Test: `internal/store/mysql/fulfillment_integration_test.go`

**Steps:**
1. Add the durable service event table and indexes.
2. Write failing integration tests for same-store check-in, time window, pending cancellation review, duplicate check-in, and cross-store access.
3. Implement row-locked check-in with the D04 window: start minus 30 minutes through slot end.
4. Verify focused integration tests with a temporary MySQL database.

### Task 3: Implement service start and completion

**Files:**
- Modify: `internal/store/mysql/fulfillment.go`
- Test: `internal/store/mysql/fulfillment_integration_test.go`

**Steps:**
1. Write failing tests for check-in -> start -> complete, repeated operations, starting after slot end, and completing after slot end when already in service.
2. Implement minimal locked transitions for `CHECKED_IN`, `IN_SERVICE`, and `COMPLETED`.
3. Verify state and event rows remain idempotent and no payment/refund totals are duplicated.

### Task 4: Implement no-show and cancellation competition

**Files:**
- Modify: `internal/store/mysql/fulfillment.go`
- Modify: `internal/store/mysql/cancellation.go`
- Test: `internal/store/mysql/fulfillment_integration_test.go`

**Steps:**
1. Write failing tests for manager-only no-show after slot end, reason persistence, pending cancellation review rejection, and concurrent cancellation/no-show with one successful CAS winner.
2. Implement the locked transition from `CONFIRMED` to `NO_SHOW`; preserve `CANCEL_REVIEW` as ineligible.
3. Verify the loser receives a domain conflict/state error and the booking never returns to an occupiable or refundable state accidentally.

### Task 5: Regression and evidence

**Files:**
- Create: `docs/dev05-progress.md`
- Create: `docs/dev05-rule-check.md`
- Modify: `README.md`
- Modify: `AGENTS.md`

**Steps:**
1. Run the focused MySQL tests with `PET_MYSQL_TEST_DSN`, then `go test -count=1 ./...`, `go vet ./...`, `go build ./cmd/pet-appointment`, prototype tests, and Docker race tests.
2. Record exact pass/fail output and keep missing HTTP pages, coverage, independent acceptance, PR, and deployment evidence explicitly blocked.
3. Review the diff for scope and run `git diff --check`.
