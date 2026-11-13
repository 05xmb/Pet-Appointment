# DEV06 HTTP Flow Implementation Plan

> **For Codex:** Execute this plan task-by-task with test-driven development and verification before claiming completion.

**Goal:** Expose the implemented cancellation, check-in, fulfillment, and no-show state transitions through authenticated HTTPS endpoints.

**Architecture:** Extend the existing `httpapi` dependency options with cancellation and fulfillment services. Each handler derives the actor from the server session, validates the role, obtains the teaching clock, and delegates all state changes to the existing transactional services. Error mapping remains explicit so conflicts are visible to clients.

**Tech Stack:** Go 1.26, `net/http`, existing session authentication and MySQL-backed services.

---

### Task 1: Add cancellation HTTP handlers

**Files:**
- Modify: `internal/httpapi/auth.go`
- Modify: `cmd/pet-appointment/main.go`
- Test: `internal/httpapi/cancellation_test.go`

**Steps:**
1. Add failing tests for owner cancellation requests, support approval/rejection, session-derived identity, and role rejection.
2. Add `WithCancellationService` and handlers for request/review.
3. Map cancellation and booking errors to 400/403/404/409 responses.
4. Run focused HTTP tests.

### Task 2: Add fulfillment HTTP handlers

**Files:**
- Modify: `internal/httpapi/auth.go`
- Modify: `cmd/pet-appointment/main.go`
- Test: `internal/httpapi/fulfillment_test.go`

**Steps:**
1. Add failing tests for check-in, service start/complete, no-show, session identity and role boundaries.
2. Add `WithFulfillmentService` and authenticated handlers.
3. Map fulfillment errors to client-visible status codes.
4. Run focused HTTP tests and existing HTTP regression tests.

### Task 3: Regression and documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/dev05-progress.md`
- Create: `docs/dev06-progress.md`

**Steps:**
1. Wire services in `cmd/pet-appointment/main.go` and run full tests, vet, build, prototype, and Docker race checks.
2. Record that pages, list/query endpoints, daily reconciliation, refund callback and formal acceptance remain open.
3. Review diff scope and run `git diff --check`.
