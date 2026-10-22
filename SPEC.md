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

## Approved Extension: Temporary OTHER Driver Submit Validation Switch

- Add the server-side Consul boolean `validate_other_driver_on_submit`, default true when absent/null. Only an explicit JSON boolean false disables submit-time Driver revalidation; reject malformed flag values rather than silently disabling validation.
- Apply the switch to OTHER IN and OUT only. With false, persist the trimmed `driverId`/`driverName` supplied by APP without a Driver lookup. Require a positive driver ID, nonblank name/note, and all existing movement validations. These driver snapshots are client-reported, not verified active at submit.
- The active-driver picker remains enabled and continues to require its configured Driver URL/token. Clients cannot disable validation through movement input. WO/SPP lookup/revalidation, unit validation, transitions, coordinates, cycle/duration behavior, and replay/hash contracts remain unchanged.
- Resolve the flag at startup and configure the movement service immutably, defaulting existing constructors to validation enabled. Emit a safe startup warning when disabled. No migration or live configuration change is part of implementation; operators explicitly set false for testing and restart the service, then restore true afterward.
- Verify safe config defaults/types, both OTHER directions with the observed upstream 404 behavior, local required-field validation, unchanged picker/document behavior, and replay compatibility. Update PRD and handovers with risks and enable/restore instructions.

## Approved Extension: Explicit WO/SPP Not-Found Responses

- Android must distinguish confirmed document absence: HTTP 404 with `WORK_ORDER_NOT_FOUND` / `WO tidak ditemukan di Workshop.` or `SPP_NOT_FOUND` / `SPP tidak ditemukan di SAOS.` Apply consistently to lookup and fresh IN/OUT submit validation; preserve replay-first handling.
- Ground absence detection in the upstream source contracts: Workshop `InternalController.getWorkOrderByNumber` returns HTTP 404, `success:false`, `code:404`, and `message:"Work order not found"`; require that combination without trusting generic proxy/route 404 pages. SAOS V2 `GetLoadingOrdersV2` returns HTTP 200/success and a list; no exact `trans_no` match means absent from this lookup, including empty or explicitly null lists (the producer returns a nil slice when no rows exist). A missing `data` field is malformed, not confirmed absence.
- Do not infer absence from missing unit/police/driver fields, a mismatched WO response number, unsuccessful envelopes, or arbitrary upstream messages. Preserve invalid-document/unit/police errors separately; network/authentication/service/JSON-envelope failures are integration errors (500), never not-found. SAOS route HTTP 404 is not its search-result contract and remains an integration failure.
- Return safe fixed public messages, distinct allowlisted `wo_not_found` / `spp_not_found` log reasons, and no upstream bodies, tokens, names, or document numbers in diagnostics. The transport's special handling of Workshop 404 must not change Driver/Unit Lambung behavior.
- No new endpoint, permission, configuration, migration, payload/hash, eligibility, movement/cycle/duration, or OTHER-switch changes. Cover producer fixtures, false-not-found cases, lookup privacy/no movement writes, submit IN/OUT, and replay. Update Swagger, APP handovers, troubleshooting, and PRD; verify tests/vet/build and Docker without live movements or live configuration writes.

## Approved Extension: SPP ID/Owner References (Supersedes Search Lookup)

- APP supplies `{id}-{owner}` to the SPP lookup `number` query and to movement request `sppNumber` for IN/OUT. Split on the first ASCII hyphen; trim both components, require each to be nonblank, preserve case/remaining owner hyphens, and encode separately with `url.Values`. SAOS URL is now `/api/v2/loading-orders?id=...&owner=...`, never `search`. Do not invent numeric constraints beyond the producer's nonblank string contract.
- Inspect the current SAOS handler/repository/model sources: ID/owner filter the linked SADP II loading order; response remains `data[]` containing canonical `trans_no`. Do not compare `trans_no` to the input reference. Resolve exactly one consistent document projection (canonical number, unit, police, driver, customer); duplicate identical projections are safe, contradictory rows fail closed as invalid document. Empty/null successful results retain `404 SPP_NOT_FOUND`; missing/malformed envelopes and infrastructure errors retain 500.
- Add an internal canonical SPP number to `movement.Document`. SPP lookup returns the original trimmed reference in `sppNumber` for reuse by APP, but reads eligibility using the upstream canonical number. Fresh submit hashes/replay-checks the original normalized request first, then uses the resolved canonical number for returning-document matching, locked eligibility, uniqueness, audit persistence, cycles, and durations. Movement/history responses contain the persisted canonical number; APP must not rebuild pending payloads from these responses.
- Reject malformed references before an upstream call; lookup returns 400 `INVALID_DOCUMENT_QUERY`, fresh submit 422 `VALIDATION_ERROR`. Parse only after the replay check on submit so already-stored legacy requests still replay unchanged. New bare document-number submissions are no longer supported by this upstream route; do not guess a legacy mapping or fall back to the removed search API. Existing open cycles can return using a reference resolving to the same canonical SPP; never rewrite audit history.
- Preserve WO/OTHER, permissions, immutable snapshots, coordinates, original request hashes/keys, and lifecycle policies. No migration/configuration is required. Verify parse/encoding/case/duplicates/ambiguity/error cases, canonical lookup/submit across IN/OUT, legacy cycles and SPP reuse guards, and pending old/new replay. Update Swagger, PRD, and standalone APP handovers; no live calls/movements/configuration edits.
