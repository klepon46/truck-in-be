# SPP Service Continuation — Operations and Design

## Scope and decision

Support a genuine breakdown return: OUT SPP-A → IN Service → OUT Filling Shed or Customer with SPP-A, including repeated Service returns. Gantungan stays Customer-only; Kosongan completes the prior SPP. A genuinely new unused SPP still permits Filling Shed.

Do not relabel Service as Gantungan, edit audit snapshots, or search arbitrary old OUT rows for justification. Existing Service history qualifies only through its own trustworthy completed same-unit cycle, matching canonical SPP and nonblank stable customer. Nullable legacy IN document/customer snapshots may use a consistent departure; contradictions or WO/driver identity conflicts fail closed. Customer display names are not identifiers.

## Database boundary

Migration `000006_add_spp_service_continuations` adds:

- Nullable FK `movement_transactions.resumed_from_service_transaction_id`, derived/verified by a BEFORE INSERT trigger. It is internal audit metadata, not an APP/API request field. No historical backfill.
- `movement_spp_return_context` and `unitlog_spp_completed(TEXT)`, shared by lookup, locked submit, and trigger validation. Trusted Service is nonterminal; untrusted Service history is not automatically exempted.
- One normal Filling Shed departure per canonical SPP, with only validated Service resumptions exempted. One departure, of either SPP destination, per source Service arrival.
- Completed cycles cannot be updated/deleted; normal open-to-completed closure still works. Immutable movements/durations remain protected.

Why not simply drop uniqueness: that would allow unrelated reuse with no Service evidence. Why not an APP flag: the database/backend must establish provenance and defend concurrency. Why not rewrite history: it destroys the actual breakdown/category/duration evidence.

Lock order: **unit snapshot → canonical-SPP advisory transaction lock** (`pg_advisory_xact_lock(8105, hashtext(spp))`). Both IN and OUT take it; Go reads eligibility in a subsequent READ COMMITTED statement after waiting. The insert trigger uses the same order. Hash collisions may serialize unrelated SPPs but do not authorize reuse. Different units sharing legacy SPP history cannot race a terminal return against continuation. Lookup is advisory/read-only, not a reservation.

The policy separately checks any prior SPP use. A historical Customer-only OUT is not a genuinely new unused SPP simply because no Filling Shed row exists.

## Controlled rollout — not performed by implementation

1. Back up the target database, record its migration version/dirty state, and rehearse this release on a disposable restored copy. Never use production DSNs for tests.
2. Pause movement writers/all replicas; coordinate a maintenance window. This transactional migration changes an index and takes table locks; it is not a zero-downtime/concurrent index build.
3. Apply migrations through **000006** once using the established environment migration runner, then verify schema/indexes and migration version.
4. Deploy this BE across all replicas before reopening writes. New BE lookup depends on the view/function. Avoid mixed versions: old BE may still reject valid Service continuations and lacks the early canonical lock.
5. Verify health and nonproduction acceptance before reopening operations. APP uses the existing lookup and dynamic destination array; no new flag, permission, Consul setting, upstream endpoint, or service token.

Production remains publish-only in Jenkins; migration/deployment belong to the manual Portainer handoff. This feature does not execute either.

## Rollback limits

Prefer stopping writers and rolling back application image while **retaining schema 000006**. The old image can read explicit existing columns, but will conservatively reject new Service continuations and may classify earlier Service history as completed. Do not claim behavioral equivalence.

Schema downgrade is permitted only with no populated continuation links and a compatible application version. The down migration takes an ACCESS EXCLUSIVE writer-excluding lock **before** checking; it refuses if even a Customer-only continuation exists. It never deletes history to restore the old Filling Shed uniqueness. Coordinate offline rollback; do not force a migration version or disable triggers after a refusal. Inspect dirty state and follow the approved migration recovery process without discarding audit data.

## Local verification

Tests enforce loopback host and dedicated database names. Example DSNs below are for a separately provisioned disposable server only:

```powershell
$env:UNITLOG_TEST_POSTGRES_DSN = 'postgres://unitlog_test@127.0.0.1:55484/unitlog_eligibility_test?sslmode=disable'
$env:UNITLOG_SERVICE_TEST_POSTGRES_DSN = 'postgres://unitlog_test@127.0.0.1:55484/unitlog_service_test?sslmode=disable'
go test ./internal/movement ./internal/httpapi -count=1
go test ./... -count=1
go vet ./...
go build ./...
```

Schema fixtures roll back; committed lifecycle/concurrency fixtures live in generated schemas removed after each test. Tests cover repeated Service/Filling/Customer/Gantungan/Kosongan, original-reference hashes and coordinates, replay, immutable earlier snapshots, normal/source uniqueness, forged markers, existing Service upgrade, terminal cross-unit locking, and downgrade racing a Customer continuation. No live movements are required.
