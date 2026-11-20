# SPP Reuse and Service Audit — Operations and Design

## Scope and decision

Policy 7 permits Filling Shed or Customer after every IN category and repeated same-unit SPP use, including after Kosongan. Prior SPP/customer/completion/cycle correlation no longer gates OUT. Customer still requires a nonblank scanned customer code, upstream/master-unit matching remains strict, and canonical SPP history on another unit blocks OUT. No-current-movement OUT remains Filling-Shed-only. See [the current APP/operations handover](mobile-spp-reuse-handover.md).

Do not relabel Service as Gantungan or edit audit snapshots. Genuine Service audit links still derive from a trustworthy completed same-unit cycle with matching canonical SPP/customer. Nullable legacy IN snapshots may use a consistent departure; contradictory snapshots do not fabricate links. Such provenance is retained for audit, not required for reuse.

Returning IN permits a different WO/SPP number for the same source type and unit. It closes its original cycle without reassigning the original departure document. A later valid same-unit SPP OUT does not need to match that IN's number/customer. The application locks both original and incoming canonical SPP keys in deterministic numeric order before closure. See [returning-IN guidance](mobile-returning-document-handover.md).

## Database boundary

Deployed migration `000006_add_spp_service_continuations` is unchanged. Its retained audit foundation includes:

- Nullable FK `movement_transactions.resumed_from_service_transaction_id`, derived/verified by a BEFORE INSERT trigger. It is internal audit metadata, not an APP/API request field. No historical backfill.
- `movement_spp_return_context` and `unitlog_spp_completed(TEXT)`; historical completion facts remain queryable but no longer block SPP OUT.
- One linked departure, of either SPP destination, per source Service arrival, with FK/shape/source uniqueness protected.
- Completed cycles cannot be updated/deleted; normal open-to-completed closure still works. Immutable movements/durations remain protected.

Migration `000007_allow_same_unit_spp_reuse` removes normal Filling Shed SPP uniqueness, replaces the insert trigger's old continuation rejection, and adds shared `unitlog_spp_used_by_other_unit(TEXT, BIGINT)` validation to lookup/submit/trigger. It derives nullable internal `spp_departure_policy_version = 7` on each new SPP OUT, without backfill or an API field. Genuine Service links and immutable history protections remain.

Why not only drop the index: backend/trigger rejection would still enforce the old policy, and cross-unit ownership would be unprotected. Why not an APP flag: policy, audit derivation and concurrency belong in the backend/database. Why not rewrite history: it destroys actual category/document/duration evidence. The version marker distinguishes new-policy departures so downgrade cannot silently discard their audit.

Lock order: **unit snapshot → canonical-SPP advisory transaction lock** (`pg_advisory_xact_lock(8105, hashtext(spp))`). Both IN and OUT take it; Go reads eligibility in a subsequent READ COMMITTED statement after waiting. The insert trigger uses the same order. Hash collisions may serialize unrelated SPPs but do not authorize cross-unit use. History checks include Customer-only history. Lookup is advisory/read-only, not a reservation.

## Controlled rollout — not performed by implementation

1. Back up the target database, record its migration version/dirty state, and rehearse this release on a disposable restored copy. Never use production DSNs for tests.
2. Pause movement writers/all replicas; coordinate a maintenance window. This transactional migration changes an index and takes table locks; it is not a zero-downtime/concurrent index build.
3. Apply migrations through **000007** once using the established environment migration runner, then verify schema/indexes and migration version.
4. Deploy this BE across all replicas before reopening writes. New BE lookup depends on the new function. Avoid mixed versions: old BE still restricts reuse and may fail against new-policy history.
5. Verify health and nonproduction acceptance before reopening operations. APP uses the existing lookup and dynamic destination array; no new flag, permission, Consul setting, upstream endpoint, or service token.

Production remains publish-only in Jenkins; migration/deployment belong to the manual Portainer handoff. This feature does not execute either.

## Rollback limits

Stop writers before considering rollback and retain schema **000007** unless an approved, compatible downgrade is possible. Older images restrict reuse and may fail on new-policy history; check the actual candidate rather than assuming image rollback is safe or behaviorally equivalent.

Downgrade 7→6 takes an ACCESS EXCLUSIVE writer-excluding lock **before** checking and refuses if any policy-7 departure exists, even a Customer-only OUT. With no such rows it restores the version-6 trigger/index while retaining old Service links. Downgrade 6→5 separately refuses populated Service links. Neither deletes history. Coordinate offline rollback; do not force a migration version or disable triggers after a refusal. Inspect dirty state and follow the approved migration recovery process without discarding audit data.

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

Schema fixtures roll back; committed lifecycle/concurrency fixtures live in generated schemas removed after each test. Tests cover repeated Filling Shed/Customer after every IN category, changed documents/customer snapshots, reference hashes/coordinates/replay, immutable history, preserved Service source uniqueness, forged markers, legacy upgrade, direct-SQL cross-unit claim races, and downgrade racing its first policy-7 Customer audit. Version-6 schema regressions remain pinned to version 6. Standalone migration batches are tested without a test-supplied outer transaction. No live movements are required.
