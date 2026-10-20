# Spec: UnitLog Backend

## Objective

Build the backend for recording immutable `IN` and `OUT` truck movements at Pool Basirih. Android satpam users scan QR codes to record movements; web operations users consume current status and audit history. Authentication is owned by Auth Service; UnitLog validates HS256 JWTs and authorizes the UnitLog permissions in the PRD.

## Tech Stack

- Go 1.25, Gin, sqlx, PostgreSQL, and golang-migrate.
- Consul KV holds flat JSON configuration. Jenkins credentials supply the Consul ACL token and deployment secrets.
- Docker builds `/app/server` and `/app/migrate`.

## Commands

```text
go test ./...
go build -o bin/server ./cmd/server
go build -o bin/migrate ./cmd/migrate
go run ./cmd/server
go run ./cmd/migrate up
docker build -t truckin-be:local .
```

## Project Structure

```text
cmd/server/       HTTP service entrypoint
cmd/migrate/      Migration entrypoint
internal/config/  Consul configuration loader
internal/database/ PostgreSQL connection setup
internal/httpapi/ Router and HTTP middleware
internal/auth/    HS256 JWT validation and permission checks
migrations/       Versioned SQL up/down migrations
tasks/            Approved implementation plan and task list
```

## Code Style

```go
func (r *Router) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
```

Use explicit dependencies, context-aware database calls, parameterized SQL, UTC timestamps, and `snake_case` database fields. API error responses must not expose internal errors or credentials.

## Testing Strategy

- Small unit tests cover config validation, JWT algorithm/claim validation, and transition rules.
- Integration tests use PostgreSQL for migration, idempotency, and concurrency behavior.
- API tests cover status codes, authorization, filtering, pagination, and export contracts.
- CI runs `go test ./...` before build and image creation.

## Boundaries

- Always: validate request input, authorize protected routes, use transactions for movement writes, and run tests before delivery.
- Ask first: alter the schema, add a dependency, change external contracts, or change Jenkins deployment behavior.
- Never: commit secrets, edit/delete an immutable transaction, use client timestamps as the transaction time, or publish Kafka events in this MVP.

## Success Criteria

- `/health` is available on port `8105` after Consul configuration and PostgreSQL are ready.
- Migrations create immutable movement transactions, unit snapshots, and sync-run audit records.
- Backend validates HS256 JWTs and the specified UnitLog permissions.
- Movement implementation later enforces the PRD transitions, idempotency, WO/SPP lifecycle, and UTC audit data atomically.
- The Docker image contains runnable `server` and `migrate` binaries; Jenkins tests, builds, and pushes it without placing secrets in the repository.

## Open Questions

- Configure the confirmed Auth Service claim names through `auth_permissions_claim`, `auth_actor_id_claim`, and `auth_actor_name_claim` before protected endpoints are exposed.
- Confirm database DSN field name in the existing Consul configuration; this spec uses `postgres_dsn`.
- Confirm Jenkins credential IDs and deployment host/runbook before enabling automatic development deployment.

## Approved Extension: Optional Movement Coordinates

- IN and OUT submissions may include one string field, `coordinates`, in `latitude,longitude` order, for every document/OTHER source. Omitted, null, empty, and whitespace-only input mean no coordinates.
- Each component is a signed decimal number (decimal point, not a decimal comma or exponent); trim whitespace around each component, preserve the supplied precision, and reject malformed pairs or values outside latitude `[-90,90]` / longitude `[-180,180]`. Limit the normalized pair to 100 characters. Zero coordinates are valid.
- Persist the normalized pair as a nullable immutable movement snapshot. Return it as `coordinates` (string or null) in movement responses/replays and authorized transaction-history JSON. No location backfill, geofencing, tracking, coordinate logging, or CSV expansion is included.
- Preserve request hashes for old inputs without coordinates. A different coordinate value under an existing idempotency key conflicts; APP must persist and retry the original payload/key without acquiring or adding new coordinates to pending submissions.
- Add a nullable-column migration before deploying the dependent application. Rollback must refuse to discard non-null coordinate audit data. Existing transitions, integration behavior (including the current SAOS V2 URL), permissions, cycles, and durations remain unchanged.
- Verify with Go validation/hash, persistence/replay, HTTP binding/privacy, operations-history, Swagger, and PostgreSQL migration/immutability checks. Update the Confluence PRD and publish a dedicated APP Agent handover.

## Approved Extension: SPP OUT Destination Eligibility

- Extend only the app-authorized SPP lookup with `allowedOutDestinations`, an always-present non-null array containing only `CUSTOMER` or `FILLING_SHED_KUIN`. Preserve the WO lookup and movement request/hash contracts, coordinates, and permissions.
- Derive current status, correlated departure SPP/customer, Filling Shed usage, and completion facts from one consistent database read. Gantungan allows only Customer when the scanned SPP and stable customer code match its trusted, completed OUT/IN cycle. No arbitrary historical OUT fallback is permitted; legacy IN records without trustworthy cycle correlation fail closed.
- Kosongan, Service, and no-history units may depart for Filling Shed only with an unused/uncompleted SPP. OUT permits no further OUT. A document's completion is evidenced by a non-Gantungan IN carrying that SPP or a correlated non-Gantungan return of its departure; prior Filling Shed usage prevents another Filling Shed departure.
- A valid document that is OUT-ineligible still returns its normal lookup data and `[]`, including for returning IN. Invalid document/unit/police errors remain errors. Database/integration failures must not become successful empty eligibility.
- Reuse the same domain policy for submit after existing idempotent replay checks and unit locking; keep uniqueness constraints as the concurrent Filling Shed backstop. Lookup is not a reservation.
- No schema change or movement mutation during lookup. Preserve the existing master-unit cache refresher; no movement, status pointer, document reservation, cycle, or duration writes are added.
- APP shows only returned destinations, clears invalid old selections, treats a missing field as unknown rather than allowing both, and never submits this derived field. Empty OUT eligibility must not block IN.
- Verify domain/SQL query behavior, HTTP array/WO contracts, error distinctions, legacy fail-closed cases, stale lookup submit rejection, and replay/coordinate regressions. Update Swagger, PRD, and a dedicated APP handover. Deploy BE before the APP filtering release; never create live test movements.
