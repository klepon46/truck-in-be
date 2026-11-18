# SPP Service Continuation — Implementation Plan

Approved scope: the Service → same-SPP Filling Shed/Customer flow in SPEC.md. Existing `tasks/plan.md`, its unchecked delivery work, and unrelated IDE files are not part of this change.

## Design

- Migration 000006 defines a trustworthy completed SPP-return projection for Service/Gantungan and a shared terminal-SPP predicate. Normal Kosongan stays terminal; only a correlated Service return is resumable.
- Add nullable `resumed_from_service_transaction_id` to new OUT rows. A BEFORE INSERT trigger derives/verifies it from the locked current master pointer and completed same-unit Service cycle, with canonical SPP/customer checks. Historical rows remain untouched.
- Retain a unique normal Filling Shed departure per SPP, exempting only validated service resumptions. A unique source-arrival index prevents consuming one Service return twice. Keep append-only protection. Down migration fails if any resumption audit link is populated.
- Use these same database facts for lookup and locked submit policy; permit Service → Customer at transition level only because SPP submit validation remains mandatory. New unused SPP from Service remains Filling-Shed-only.
- Lock order is unit snapshot → canonical-SPP advisory transaction lock (namespace 8105). Acquire before reading eligibility on submit, for both IN and OUT; the insert trigger uses the same order. This serializes global completion across units, not just duplicate scans of one unit. Distinguish any previous canonical SPP use from Filling-Shed-only use, so an unmatched legacy Customer departure cannot look like a new SPP.
- Reject conflicting WO/driver identity on both arrival and departure. Validate supplied marker fields on every insertion shape. Protect completed cycle correlation from subsequent UPDATE/DELETE while retaining normal open-to-completed closure.

## Slices / Checkpoints

1. **Schema foundation:** failing disposable PostgreSQL tests for validated resumption metadata, constrained uniqueness, terminal semantics and safe downgrade; implement migration. Verify real constraints/rollback and unchanged snapshots.
2. **Eligibility path:** failing policy/SQL/HTTP tests; update shared state query, transition and submit policy. Verify same-SPP Service destinations, new-SPP behavior, legacy/invalid/terminal negatives and existing Gantungan/WO/OTHER.
3. **End-to-end:** disposable service lifecycle including repeated breakdowns, alias references, replay, concurrent submissions, completion and immutable prior snapshots/durations.
4. **Handover/review:** Swagger, standalone APP guide, operations migration/rollback notes, existing guides and PRD; full Go/vet/build, Docker checks and adversarial review.

## Risks

- An unvalidated non-null marker must not bypass uniqueness: derive/validate it in the database rather than accept an APP flag.
- Invalid Service history must not stop being terminal just because its category is Service: require trusted completed-cycle correlation.
- Rollback must preserve continuation audit metadata and must not force old uniqueness by deleting history.
- Local PostgreSQL fixtures only; never use an application/production DSN. Do not modify the unrelated `.idea/dataSources.xml`.

## Verification

`go test ./internal/movement ./internal/httpapi -count=1`, disposable PostgreSQL tests, `go test ./... -count=1`, `go vet ./...`, `go build ./...`, generated Swagger checks, and Docker builds where available.
