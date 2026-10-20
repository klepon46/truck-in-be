## Task 1: Service Foundation

**Description:** Create the Go module, Consul configuration loader, PostgreSQL startup check, health endpoint, and migration executable.

**Acceptance criteria:**
- [x] Service fails startup when required Consul configuration or PostgreSQL connection is unavailable.
- [x] `GET /health` returns `200` after startup.
- [x] Configuration and errors never expose tokens or DSNs.

**Verification:**
- [x] `go test ./...`
- [x] `go build -o bin/server ./cmd/server`
- [x] `go build -o bin/migrate ./cmd/migrate`

**Dependencies:** None

**Files likely touched:** `cmd/`, `internal/config/`, `internal/database/`, `internal/httpapi/`, `go.mod`

**Estimated scope:** Medium

## Task 2: Initial Schema

**Description:** Define PostgreSQL tables and indexes for snapshot, immutable movement, and sync audit data.

**Acceptance criteria:**
- [x] `movement_transactions` has no update/delete permissions in the application design and stores all audit fields.
- [x] Snapshot and sync tables contain the PRD fields and key indexes.
- [x] Up and down migrations are present.

**Verification:**
- [ ] Apply and roll back the migration against PostgreSQL (requires deployment PostgreSQL).
- [x] `go test ./...`

**Dependencies:** Task 1

**Files likely touched:** `migrations/`

**Estimated scope:** Small

## Task 9: Optional Coordinate Validation And Legacy Hash Compatibility

- [x] Accept/normalize a nullable-or-empty optional `latitude,longitude` string for IN/OUT; reject invalid format/range without logging location.
- [x] Prove legacy request hashes do not change and changed coordinates conflict on replay.
- Verify: `go test ./internal/movement -count=1`.
- Dependencies: approved coordinate contract in `SPEC.md`.
- Scope: `internal/movement/validation.go` and focused coordinate tests.

## Task 10: Coordinate Audit Persistence And HTTP Contract

- [x] Add migration 000005 with immutable nullable coordinates and safe rollback; persist/replay string-or-null values.
- [x] Bind the app request and expose coordinates in authorized transaction-history JSON, not unit pickers or CSV.
- Verify: focused movement/HTTP/operations tests and PostgreSQL migration/immutability checks when available.
- Dependencies: Task 9. Scope: migration, movement service, HTTP handler, operations history, and matching tests.

## Task 11: Coordinate APP Handover And PRD

- [x] Publish a dedicated APP Agent guide covering optional acquisition, format, retries, null handling, errors, and deployment order; update existing handovers and PRD (Draf v1.13 / Confluence version 19).
- [x] Regenerate Swagger and complete `go test ./...`, `go vet ./...`, `go build ./...`, Docker test image, PostgreSQL migration checks, and diff review.
- Dependencies: Task 10. Scope: generated API docs, handovers, and Confluence PRD.
