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

- `/health` is available on port `8104` after Consul configuration and PostgreSQL are ready.
- Migrations create immutable movement transactions, unit snapshots, and sync-run audit records.
- Backend validates HS256 JWTs and the specified UnitLog permissions.
- Movement implementation later enforces the PRD transitions, idempotency, WO/SPP lifecycle, and UTC audit data atomically.
- The Docker image contains runnable `server` and `migrate` binaries; Jenkins tests, builds, and pushes it without placing secrets in the repository.

## Open Questions

- Configure the confirmed Auth Service claim names through `auth_permissions_claim`, `auth_actor_id_claim`, and `auth_actor_name_claim` before protected endpoints are exposed.
- Confirm database DSN field name in the existing Consul configuration; this spec uses `postgres_dsn`.
- Confirm Jenkins credential IDs and deployment host/runbook before enabling automatic development deployment.
