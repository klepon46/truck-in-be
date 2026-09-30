# UnitLog Backend

UnitLog records immutable `IN` and `OUT` truck movements for Pool Basirih. Android satpam users scan a unit QR code to submit a movement; web operations users view current unit status, duration, history, and CSV exports. The service owns unit snapshots and movement history. Authentication and role management remain the responsibility of Auth Service.

## Capabilities

- Record atomic, immutable `IN` and `OUT` movements from scanned unit QR codes.
- Enforce transition rules, work-order and SPP lifecycle rules, and request idempotency.
- Synchronize active unit snapshots from Unit Lambung at startup and at a configured interval.
- Refresh an unknown scanned unit directly from Unit Lambung before rejecting it.
- Provide dashboard, unit monitoring, transaction history, filtering, pagination, and CSV exports for operations users.
- Store transaction timestamps in UTC and return an audit trail that cannot be edited or deleted.

## Architecture

| Component | Responsibility |
| --- | --- |
| Go, Gin, sqlx | HTTP service and data access |
| PostgreSQL | Unit snapshots, immutable movements, and synchronization-run audit records |
| Consul KV | Environment-specific flat JSON application configuration |
| Auth Service | HS256 JWT issuance, authentication, and role management |
| Unit Lambung | Authoritative unit snapshot source |
| Workshop, SAOS, Driver | Work-order, SPP, and active-driver validation |
| golang-migrate | Versioned PostgreSQL migrations |
| Docker and Jenkins | Container delivery and environment deployment |

## Access Control

All `/api/v1` routes require an Auth Service HS256 JWT. The service reads the configured permissions and actor claims from the token.

| User | Permission | Allowed routes |
| --- | --- | --- |
| Android satpam | `unitlog:app:default:view` | `POST /api/v1/movements` |
| Web operations | `unitlog:web:default:view` | Dashboard, unit, transaction, and CSV export routes |

## Movement Rules

The client sends the final QR URL segment as `noLambung`. For example, `https://example.invalid/SAMT223` becomes `SAMT223`.

### IN

An `IN` movement requires exactly one category:

- `WAITING_FOR_ASSIGNMENT`
- `WAITING_FOR_DELIVERY_TO_CUSTOMER`
- `SERVICE_AND_REPAIR_MAINTENANCE`

`WAITING_FOR_DELIVERY_TO_CUSTOMER` retains the unit's active SPP. Any other `IN` category completes the active SPP for that movement cycle.

### OUT

| Destination | Required input | Driver source | Notes |
| --- | --- | --- | --- |
| `BENGKEL_LUAR` | `workOrderNumber` | Workshop work order | Work order must exist and can be used once. |
| `FILLING_SHED_KUIN` | `sppNumber` | SAOS SPP | Activates the SPP for the unit. |
| `CUSTOMER` | `sppNumber` | SAOS SPP | Requires the same active SPP and customer. |
| `OTHER` | `driverId`, `driverName`, `note` | Active Driver result | A note is mandatory. |

A unit with current status `OUT` must record `IN` before another `OUT`. For valid submissions, the service locks the unit snapshot, validates the transition and external document, inserts the immutable transaction, updates the current pointer, and commits as one PostgreSQL transaction.

Clients must send an `Idempotency-Key` UUID for every movement. Retrying the same request with the same key returns the prior result; reusing a key with a different payload is rejected.

## Requirements

- Go 1.25+
- PostgreSQL
- Consul access to the environment-specific configuration key
- Docker, if building or running the container image

## Configuration

The application reads one flat JSON value from Consul at startup. It does not read application secrets from local files or command-line arguments.

Set these deployment environment variables:

```text
CONSUL_HTTP_ADDR=https://consul.example.internal
CONSUL_CONFIG_KEY=dev/be/truckin-be-config
CONSUL_HTTP_TOKEN=<optional Consul ACL token>
```

Use `dev/be/truckin-be-config` for development and `prod/be/truckin-be-config` for production. The Consul value must contain all runtime fields below. Replace every placeholder with an environment-specific value in Consul; never commit secrets, DSNs, or service tokens.

```json
{
  "server_port": 8105,
  "postgres_dsn": "<postgresql-dsn>",
  "auth_jwt_hs256_secret": "<auth-service-hs256-secret>",
  "auth_permissions_claim": "<permissions-claim>",
  "auth_actor_id_claim": "<actor-id-claim>",
  "auth_actor_name_claim": "<actor-name-claim>",
  "unit_sync_interval": "6h",
  "unit_sync_page_size": 100,
  "lambung_api_base_url": "<lambung-base-url>",
  "lambung_service_token": "<lambung-service-token>",
  "saos_api_base_url": "<saos-base-url>",
  "saos_service_token": "<saos-service-token>",
  "workshop_api_base_url": "<workshop-base-url>",
  "workshop_service_token": "<workshop-service-token>",
  "driver_api_base_url": "<driver-base-url>",
  "driver_service_token": "<driver-service-token>",
  "http_client_connect_timeout_ms": 3000,
  "http_client_request_timeout_ms": 10000
}
```

Consul HTTPS is required by default. An internal HTTP Consul instance requires an explicit opt-in:

```text
CONSUL_HTTP_ADDR=http://consul.example.internal:8500
CONSUL_CONFIG_KEY=dev/be/truckin-be-config
CONSUL_ALLOW_INSECURE_HTTP=true
```

HTTP sends configuration secrets over the network without TLS. Use HTTPS whenever possible. `CONSUL_ALLOW_INSECURE_HTTP` accepts only `true` or `false`.

## Local Development

Run the commands from the repository root after PostgreSQL and Consul configuration are available:

```text
make test
make build
make migrate-up
make run
```

Equivalent Go commands:

```text
go test ./...
go build -o bin/server ./cmd/server
go build -o bin/migrate ./cmd/migrate
go run ./cmd/migrate up
go run ./cmd/server
```

The migration command supports `up` and `down [steps]`. Set `MIGRATIONS_PATH` when migration files are not located in `./migrations`.

## HTTP API

`GET /health` is unauthenticated. It returns `200` only after startup succeeds and PostgreSQL is reachable; it returns `503` when the database cannot be reached.

| Route | Permission | Purpose |
| --- | --- | --- |
| `POST /api/v1/movements` | Android | Record an immutable movement. Requires `Idempotency-Key`. |
| `GET /api/v1/dashboard` | Web | Get active-unit dashboard totals. |
| `GET /api/v1/units` | Web | List units and current status. |
| `GET /api/v1/units/export` | Web | Export the matching unit view as CSV. |
| `GET /api/v1/units/{id}` | Web | Get unit detail and current status. |
| `GET /api/v1/units/{id}/transactions` | Web | List a unit's transaction timeline. |
| `GET /api/v1/transactions` | Web | List the immutable transaction audit trail. |
| `GET /api/v1/transactions/export` | Web | Export the matching transaction view as CSV. |

Unit lists support `query`, `status`, `destination`, `includeInactive`, `page`, and `pageSize`. Transaction lists support `query`, `direction`, `detail`, `from`, `to`, `page`, and `pageSize`. Date filters use RFC3339 timestamps; pagination is 1-based, defaults to 25 items, and has a maximum page size of 100.

Successful list responses contain `data` and `pagination`. API errors use this shape:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "movement input is invalid"
  }
}
```

### Swagger

After startup, open `http://localhost:8105/swagger/index.html`. Use **Authorize** to provide `Bearer <Auth Service JWT>` for protected routes. The generated OpenAPI document is available at `/swagger/doc.json`.

Regenerate committed Swagger files after changing the HTTP contract:

```text
go install github.com/swaggo/swag/cmd/swag@v1.16.4
make swagger
```

## Unit Synchronization

The service synchronizes Unit Lambung once at startup and then at `unit_sync_interval`. It consumes the cursor-paginated endpoint:

```http
GET ${lambung_api_base_url}/lambung/v1/internal/unit-snapshots?limit={1..200}&cursor={opaque-cursor}
Authorization: Bearer ${lambung_service_token}
```

The first request omits `cursor`. A response contains `data.items`, `data.nextCursor`, and `data.hasMore`; the final page must return `hasMore: false` and `nextCursor: null`. UnitLog rejects malformed pages, repeated cursors, duplicate unit IDs, and units without a positive ID, unit number, or plate number.

Only a completed successful traversal updates snapshots and marks units absent from the complete result inactive. A failed or incomplete synchronization does not deactivate existing units. See [the Unit Lambung synchronization contract](docs/lambung-unit-snapshot-sync.md) for the full producer contract.

## Docker and Delivery

Build the application image locally:

```text
docker build --tag truckin-be:local .
```

The image contains `/app/server`, `/app/migrate`, and the migration files. It listens on port `8105` and includes a `/health` Docker health check. The default entrypoint starts the server. Run migrations with:

```text
docker run --rm --entrypoint /app/migrate <image> up
```

Inject the Consul environment variables when running either binary in a container.

`Jenkinsfile` accepts `TARGET_ENV=dev` or `TARGET_ENV=prod`. It regenerates and checks Swagger, builds the Docker test target, builds and tags the image, and pushes it with the Jenkins agent's configured Docker registry authentication. Development deployments run on the Jenkins agent with health checks and rollback behavior. Production requires manual approval and only publishes the image; production migration and deployment follow the production runbook.

## Project Layout

```text
cmd/server/          HTTP service entrypoint
cmd/migrate/         Migration command entrypoint
internal/auth/       HS256 JWT authentication and permission checks
internal/config/     Consul configuration loader and validation
internal/database/   PostgreSQL connection setup
internal/httpapi/    Router, handlers, CSV export, and OpenAPI annotations
internal/integration/ External-service clients
internal/movement/   Atomic movement validation and persistence
internal/operations/ Dashboard, unit, and transaction queries
internal/unitsync/   Cursor-based Unit Lambung synchronization
migrations/          Versioned SQL migrations
docs/                Generated Swagger and integration contracts
```

## MVP Boundaries

- UnitLog does not create QR codes.
- UnitLog does not implement login or role management.
- Stored movements cannot be edited, deleted, or manually corrected.
- There is no manual movement fallback when external integrations are unavailable.
- Kafka publishing, retry, and event recovery are outside this MVP.
- Workshop validation is limited to finding the matching work order.
- SAOS and Driver endpoint changes are owned by their respective services.

## References

- [Product requirements document](https://sadp-team.atlassian.net/wiki/spaces/ST/pages/220495876/Unit+Log+TRY-In+Out+Basirih+Pool)
- [Local implementation specification](SPEC.md)
- [Unit Lambung synchronization contract](docs/lambung-unit-snapshot-sync.md)
- [Gin](https://gin-gonic.com/en/docs/quickstart/)
- [sqlx](https://github.com/jmoiron/sqlx)
- [golang-migrate](https://github.com/golang-migrate/migrate)
- [Viper](https://github.com/spf13/viper)
