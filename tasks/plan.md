# Implementation Plan: UnitLog Backend

## Overview

Initialize the Go service, then implement UnitLog in vertical slices: safe startup, authenticated movement recording, master-unit synchronization, and operational read/export APIs.

## Architecture Decisions

- Consul is read at startup through its KV HTTP API; its address, ACL token, and JSON key are injected by the deployment environment.
- PostgreSQL is authoritative for snapshots and immutable movement history. A single database transaction will lock a unit snapshot, validate the transition, insert the history record, and update the current pointer.
- JWT validation is local and restricted to HS256. Permission checks are route-specific.
- `golang-migrate` executes versioned SQL files from the image filesystem.

## Task List

### Phase 1: Foundation (complete)

- [x] Task 1: Initialize the Go service, Consul configuration, health endpoint, and migration binary.
- [x] Task 2: Add the initial PostgreSQL schema for snapshots, immutable movement history, and sync runs.
- [ ] Checkpoint: Service builds, migrations run, and `/health` is ready only with valid configuration/database access.

### Phase 2: Movement Recording (complete)

- [x] Task 3: Implement JWT authorization and request validation for satpam movement scans.
- [x] Task 4: Implement unit lookup/refresh and transactional IN/OUT validation with idempotency.
- [x] Task 5: Implement Workshop, SAOS, and Driver clients with bounded timeouts.
- [ ] Checkpoint: valid scans persist once; invalid transitions and changed idempotency payloads are rejected.

### Phase 3: Operations APIs (complete)

- [x] Task 6: Add scheduled and startup Unit Lambung synchronization with incomplete-sync protection.
- [x] Task 7: Add dashboard, monitoring, unit detail, history, and CSV export read APIs.
- [ ] Checkpoint: status/duration/history results match transaction fixtures and filters.

### Phase 4: Delivery

- [ ] Task 8: Complete Jenkins deployment integration after credential IDs, deploy target, and production runbook are confirmed.
- [ ] Checkpoint: CI test/build/push succeeds and deployment rollback is demonstrated in development.

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Concurrent scans create invalid status | High | Lock `unit_snapshot` within one transaction and test concurrent submissions. |
| External document APIs are unavailable | High | Apply fixed connect/request timeouts and fail the scan without a partial write. |
| SAOS lacks customer fields | Medium | Gate Customer comparison on fields being available; preserve documented dependency. |
| Secrets leak through config/logs | High | Load at runtime only; redact errors and ignore secret files. |

## Open Questions

- JWT permission and identity claim names.
- Database DSN key and the Jenkins deployment credential IDs.

## Approved Extension: Optional Movement Coordinates

1. Add validation/normalization for one optional `coordinates` string and regression tests proving legacy request hashes remain unchanged.
2. Add migration 000005 and movement input/persistence/replay support, then expose nullable coordinates in authorized transaction-history JSON. Verify SQL and HTTP behavior without changing transitions, cycles, durations, permissions, CSV output, or the user-owned SAOS V2 adjustment.
3. Regenerate Swagger, publish the APP Agent coordinate handover, update the existing handovers and Confluence PRD, and run the full Go and available PostgreSQL checks.

Primary risk: silently breaking retries created by older apps. Mitigation: omit an empty coordinate field from the internal hash representation and test a frozen legacy payload hash. Apply migration before application deployment; never discard populated coordinate audit data during rollback.

## Approved Extension: SPP OUT Eligibility

1. Add a cycle-correlated SPP state reader and shared OUT policy, with unit tests and isolated PostgreSQL query verification. Distinguish missing correlation/eligibility from database failures; preserve legacy audit data.
2. Add the SPP-only response array and use the shared policy under the submit lock after replay checks. Cover valid returning-IN lookups, completed/used SPP, and status changes after lookup. Preserve WO, coordinates, request hashes, SAOS V2, and existing permissions.
3. Publish an APP eligibility handover, update the PRD and generated Swagger, then complete Go tests/vet/build, Docker tests, and isolated PostgreSQL checks.

No migration is required. Main risks are deriving active SPP from unrelated old history, mixing status reads from different snapshots, and breaking replays after a successful OUT; the shared fact query and replay-first submit order address these. Staged user-owned prompt files must remain outside agent commits.
