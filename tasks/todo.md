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

## Task 12: Cycle-Correlated SPP State And Domain Policy

- [x] Read current status, trusted departure SPP/customer, prior Filling Shed use, and SPP completion from a consistent database snapshot.
- [x] Test Customer/Fill eligibility and legacy fail-closed cases; preserve database errors (15 isolated PostgreSQL scenarios passed, including conflicting arrival SPP and trusted legacy cycles).
- Verify: focused domain tests and isolated PostgreSQL fixtures; no live movements.
- Dependencies: approved SPP eligibility contract in `SPEC.md`. Scope: a focused domain file and tests.

## Task 13: SPP Lookup Array And Atomic Submit Revalidation

- [x] Always return a non-null eligibility array for valid SPP lookups; preserve returning IN and the WO contract.
- [x] Use the same policy on submit after replay checks and unit locking; reject stale eligibility without side effects.
- Verify: movement/HTTP/authorization/coordinate/replay tests.
- Dependencies: Task 12. Scope: movement service, lookup handler/interface, and relevant regression tests.

## Task 14: SPP Eligibility APP Handover And PRD

- [x] Document filtering, missing versus empty arrays, selection clearing, IN behavior, legacy correlation, and BE-first deployment in APP handovers and PRD (Draf v1.14 / Confluence version 20).
- [x] Regenerate Swagger; complete Go tests/vet/build, Docker test image, PostgreSQL query checks, and review.
- Dependencies: Task 13. Scope: Swagger, handovers, PRD, and verification.

## Task 15: Safe OTHER Driver Validation Configuration

- [x] Decode `validate_other_driver_on_submit` with true-by-default behavior and reject nonboolean values.
- [x] Configure the movement service at construction; skip Driver revalidation only for valid OTHER IN/OUT when false.
- [x] Keep positive driver IDs, name/note, document checks, and driver picker configuration required.
- Verify: config and movement unit tests. No migration or live configuration writes.

## Task 16: OTHER Submit And Picker Regression Coverage

- [x] Reproduce partial-name picker success followed by full-name submit 404; prove false skips the submit call for both directions while the picker still works.
- [x] Verify immutable snapshots, unit/transitions/return-source checks, cycles/durations, replay and coordinates remain enforced.
- Verify: focused HTTP/movement tests and complete Go suite.
- Dependencies: Task 15.

## Task 17: OTHER Switch PRD And Handover

- [x] Document test-only activation/restoration in Consul, startup restart requirement, client-reported snapshot risks, and unchanged APP payloads.
- [x] Update PRD (Draf v1.15 / Confluence version 21, read-back verified), relevant handovers, and Swagger descriptions; pass Go tests/vet/build, Docker test image, and focused correctness/security review. Local HTTP tests reproduce picker success/full-name 404 and verify both bypass directions without live movements.
- Dependencies: Task 16. Preserve user-owned staged/untracked documents.

## Task 18: Upstream Document Absence Classification

- [x] Add typed WO/SPP absence errors and decode the verified Workshop 404 / SAOS successful no-exact-match contracts.
- [x] Distinguish incomplete documents and transport/envelope failures; preserve generic Driver/Unit Lambung behavior.
- Verify: failing-then-passing integration classification tests; `go test ./internal/integration -count=1`.
- Scope: integration client/tests and movement error declarations.

## Task 19: Android Not-Found HTTP Contract

- [x] Preserve document-specific errors through lookup and submit; return HTTP 404 with fixed public codes/messages and allowlisted log reasons.
- [x] Verify both documents/directions, no writes on failure, safe diagnostics, and unchanged replay (42 local HTTP scenarios plus IN/OUT replay/conflict tests; Go tests/vet/build passed).
- Verify: focused HTTP/movement tests, then `go test ./... -count=1`.
- Dependencies: Task 18. Scope: movement service and HTTP handlers/tests.

## Task 20: Document Errors Handover And Verification

- [x] Update Swagger, standalone APP handover, troubleshooting, related handovers, and PRD (Draf v1.16 / Confluence version 22, read-back verified).
- [x] Pass full Go tests/vet/build, Docker test image, JSON/Swagger contract checks, and correctness/security review; no live mutations or configuration changes. Source contracts were read locally, not verified against deployed upstream instances.
- Dependencies: Task 19. Preserve unrelated user work.

## Task 21: SPP Reference Parser And Adapter

- [x] Parse `{id}-{owner}` with first-hyphen splitting, trimming, case preservation, separate encoding, and malformed-input rejection.
- [x] Resolve a canonical `trans_no` from consistent SAOS records; keep not-found/invalid/integration error distinctions and reject ambiguity.
- Verify: failing-then-passing integration tests. Scope: adapter, domain document field, and integration fixtures/tests.

## Task 22: Canonical SPP Lifecycle With Original Request Replays

- [x] Use canonical SPP for lookup eligibility, return matching, locked submit eligibility, unique reuse checks, audit/cycles/durations; echo reference for lookup/APP submit.
- [x] Hash/replay original payload before resolving; prove legacy and new retries/coordinate snapshots survive and conflicting payloads still fail. The disposable PostgreSQL lifecycle test passed: legacy open OUT → reference IN Gantungan → aliased OUT Customer → reference final IN; mismatch/reuse attempts add no movements and final counts are 4 movements / 3 durations / 2 closed cycles.
- Verify: movement/HTTP tests plus isolated PostgreSQL end-to-end fixture where available. Dependencies: Task 21.

## Task 23: SPP Reference APP Contract And Verification

- [ ] Update Swagger, PRD, related guides, and standalone APP handover with input-reference versus canonical-audit semantics and deployment/retry limits.
- [ ] Pass Go tests/vet/build, Docker tests, and review. No live mutations, calls, or configuration changes.
- Dependencies: Task 22. Preserve unrelated work.
