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
