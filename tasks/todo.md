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

- [x] Update Swagger, PRD (Draf v1.17 / Confluence version 23, read-back verified), related guides, and standalone APP handover with input-reference versus canonical-audit semantics and deployment/retry limits.
- [x] Pass Go tests/vet/build, Docker test image, JSON/Swagger checks, disposable PostgreSQL lifecycle/replay test, and correctness/security review. No live integration calls, movements, or configuration changes; no schema/config changes required.
- Dependencies: Task 22. Preserve unrelated work.

## Task 24: Specific Missing SPP Unit Message

- [x] Preserve an internal missing-SPP-unit cause and expose the approved message on lookup and submit IN/OUT, retaining 422/code and strict rejection.
- [x] Test missing/null/blank/whitespace unit codes and negative cases; pass full tests/vet/build and review without live writes (12 new missing-unit HTTP cases, generic other-field/WO checks, and existing master-unit regressions).
- [x] Update Swagger, PRD (Draf v1.18 / Confluence version 24, read-back verified), and APP guidance; upstream queries/master data remain unchanged. Docker test image and exact-message Swagger checks also passed.
- Verify: focused HTTP/integration regressions followed by `go test ./... -count=1`, `go vet ./...`, and `go build ./...`.

## Task 25: SPP Uses Master Unit Police Number

- [x] Add failing adapter/HTTP regressions for ignored SAOS police text, omissions and duplicate differences; verify lookup and successful IN/OUT use master police values (eight police variants, including unused non-string data).
- [x] Relax only SPP police checks; preserve required unit/active master resolution, wrong-unit rejection, WO validation, replay/history and lifecycle rules. Focused/full Go tests passed, plus disposable PostgreSQL eligibility and canonical lifecycle/replay tests with unchanged old police and master-backed new snapshots. The temporary PostgreSQL server was stopped; no live data touched.
- Verify: focused integration/HTTP/movement tests, then `go test ./... -count=1`. Files: adapter/service and their focused tests. Dependencies: Task 24.

## Task 26: SPP Police Policy Handover And Verification

- [x] Update Swagger, standalone APP handovers/troubleshooting and PRD (Draf v1.19 / Confluence version 25, read-back verified) with master authority, unchanged payloads/history, and the lost independent cross-check. Mark historical prompt police requirements as superseded.
- [x] Pass full Go tests/vet/build, generated Swagger contract checks and disposable PostgreSQL eligibility/lifecycle/replay tests; review correctness/security/readability/architecture/performance. No new query/dependency/schema/config or public request fields; only trusted canonical SPP documents relax police checks. Docker verification could not run because the local Docker daemon is unavailable; PostgreSQL 15 disposable local fixtures passed and the test server was stopped. No live calls/writes, push, or deployment.
- Verify: generated Swagger contract/read-back PRD and `go test ./... -count=1`, `go vet ./...`, `go build ./...`. Dependencies: Task 25.

## Task 27: Keep Jenkins PROD Publish-Only

- [x] Reproduce the missing PROD migration guard with a failing local pipeline contract test; add a DEV-only guard and preserve approval/push/DEV runtime forwarding and rollback. Focused tests passed after the fix.
- [x] Add an explicit published-not-deployed production handoff with the exact image, Portainer runbook, and non-secret Consul environment values. No token values are logged or baked into the image.
- Verify: `go test ./ci -count=1`, then full Go tests/vet/build. Scope: Jenkinsfile and a focused test file. No live Jenkins/registry/deploy operations.

## Task 28: Manual PROD Portainer Runbook

- [x] Document runtime Consul Env, manual migration-before-start, server recreation, health verification, and safe rollback; link from README and Jenkins output (`docs/production-portainer.md`).
- [x] Review correctness/security/scope and source/runbook consistency; full Go tests/vet/build passed after rerunning a timed-out foreground check in the background. Docker verification is unavailable because the local daemon is not running; Jenkins controller/Portainer execution was not performed. The stage guard follows the existing DEV condition and official Jenkins `when` documentation. No live Jenkins, registry, Consul, database, or Portainer changes were made.
- Verify: source/runbook consistency, `go test ./... -count=1`, `go vet ./...`, `go build ./...`; Docker where available. Dependencies: Task 27.

## Task 29: Preserve Service Prefixes In Integration URLs

- [x] Reproduce dropped base paths with failing HTTP request tests, including trailing-slash, nested/escaped prefix and encoded query cases across Lambung, SAOS, Workshop, and Driver (32 request-matrix cases plus six prefixed status/not-found cases).
- [x] Join endpoint paths beneath the configured service root using documented Go `URL.JoinPath`/`EscapedPath`; preserve bare-origin behavior, auth headers, transport/TLS, and safe status/not-found classifications. Focused integration and full Go tests/vet/build passed; no transport/auth/response-policy changes.
- Verify: `go test ./internal/integration -count=1` followed by full Go tests/vet/build. Scope: shared integration client and focused regression tests. No live requests/configuration/deploy changes.

## Task 30: Prefix Deployment Guidance And Verification

- [x] Update configuration, Unit Lambung sync, and Portainer guidance with prefix-aware URLs, status interpretation, and deployment/verification limits. Existing paths on other integrations are now honored; review these before rollout. No live requests/configuration/token/TLS changes.
- [x] Review correctness/security/architecture/readability/performance and source/documentation consistency. Full Go tests/vet/build, Docker test target, and production-image build passed. The shared change joins escaped paths only and retains endpoint queries, headers, transport/TLS, timeout/context, privacy, and response classifications. Production token validity and the original generic transport failure remain unverified; no live configuration, integration, data, push, or deployment changes.
- Verify: source/documentation consistency and full test results; report unavailable tooling. Dependencies: Task 29.

## Task 31: Returning IN By Same Document Type And Unit

Approved contract: `SPEC.md` → Returning IN Matches Document Type And Unit, Not Number. Previous task/checkpoint statuses stay unchanged.

- [x] Prove changed-number WO/SPP IN with failing tests; retain same-type/same-unit/upstream validation, immutable OUT and original departure metadata. Three changed-number cases failed before implementation; unit, real PostgreSQL category/source/replay/rollback tests and full Go tests/vet/build now pass.
- [x] Preserve canonical completion serialization when IN changes SPP: lock incoming/outgoing SPPs in deterministic advisory-key order; tested lifecycle, replay, rollback, wrong-unit/cross-type and concurrency on disposable PostgreSQL. Both original/incoming lock waits and concurrent reverse A→B/B→A returns pass; HTTP tests verify actual upstream unit fields and distinct IN/departure response numbers. Full tests/vet/build passed with all three disposable PostgreSQL DSNs enabled.
- [x] Update Swagger and standalone APP guidance, reconcile old same-number wording, and review/test full Go/vet/build/Docker availability. Added `docs/mobile-returning-document-handover.md`, regenerated Swagger, and updated affected guides. Correctness/security/architecture/readability/performance review and guard mutation check passed. Final Go tests/vet/build passed with disposable schema/lifecycle tests; the existing reference lifecycle also passed with all three DSNs enabled before documentation-only edits. Docker verification is blocked because the local daemon is not running; no Docker success is claimed. Disposable PostgreSQL stopped. No schema, OUT policy, live data/configuration, push, or deployment changes.

## Task 32: Open SPP Destinations And Permit Same-Unit Reuse

Approved contract: the latest change in `SPEC.md`. Earlier SPP task statuses record historical delivery; their old terminal/Customer-only rules are superseded, not silently retained.

- [x] Prove repeat-use and all-IN destination cases RED; implement migration 000007 with shared cross-unit guard, derived version audit, preserved Service links, unchanged 000006, and downgrade refusal once new-policy records exist. Nine policy cases failed before implementation; version-6 Filling Shed rejection is reproduced before upgrade.
- [x] Align transition, lookup and locked submit; adapt superseded assertions while preserving version-6 schema/audit regressions and unchanged unit/source/WO/OTHER/replay behavior. Customer retains its nonblank scanned customer requirement; no-history OUT remains Filling-Shed-only. Guard inversion was caught by the policy tests.
- [x] Verify repeated Filling Shed/Customer after every IN category, changed scan numbers, immutable histories, cross-unit concurrency, rollback and safe upgrade/downgrade on disposable PostgreSQL. Includes direct-SQL claim races, downgrade waiting on its first new-policy Customer audit, preservation of populated legacy Service links, and standalone multi-statement migration batches. Go tests/vet/build passed; existing reference lifecycle also passed with all three disposable DSNs enabled.
- [x] Regenerate Swagger and update APP/operations guidance, including `docs/mobile-spp-reuse-handover.md` and migration-before-BE/guarded rollback. Self-review found no remaining blocking issues; policy guard mutation checks are recorded above. Final full Go tests with disposable schema/lifecycle DSNs, vet/build, Docker test target and production-image build passed. Swagger changes are descriptions only; payload/schema/security contracts remain unchanged, and local documentation links were checked. Historical migration 000006 and user IDE files remain untouched. Disposable PostgreSQL stopped. No production writes, push, or deployment; Confluence was not updated.
