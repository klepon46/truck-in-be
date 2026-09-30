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
