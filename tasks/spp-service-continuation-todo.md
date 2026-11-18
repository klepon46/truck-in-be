# SPP Service Continuation Checklist

- [x] S1 — Add failing schema/SQL tests; implement migration 000006 with derived resumption references, normal/exception uniqueness, shared return/completion facts, and fail-closed downgrade. Verified disposable PostgreSQL: forged markers/identities, normal/source uniqueness, immutable history/cycles, empty downgrade and populated-link refusal.
- [x] S2 — Add failing Service continuation policy/transition/HTTP regressions, then align lookup and submit; retain fresh-SPP Filling Shed, Gantungan Customer-only, terminal/reuse and invalid-source rejection. Canonical-SPP lock precedes fresh IN/OUT validation; policy mutation was caught by tests. Full Go tests/vet/build passed.
- [x] S3 — Prove repeated Service continuation, original-reference replay/coordinates, concurrent submits, terminal completion, historical immutability and migration rollback against real disposable PostgreSQL. Includes distinct/same-key same-unit races, IN/OUT canonical locking, terminal commit on another unit, cycle-failure rollback, pre-migration Service, and downgrade waiting for Customer audit.
- [x] S4 — Update Swagger, APP/operations guides and PRD; complete adversarial review and Go tests/vet/build/Docker verification. Design review findings addressed: global canonical locking, any-prior-use, all-shape marker guards, departure identity, downgrade writer lock, completed-cycle immutability. Final correctness/readability/architecture/security/performance review found no remaining blocker; policy mutation test detected the inverted Service condition. No production writes, pushes, or deployment.

## Verification / Handover

- Full `go test ./... -count=1`, `go vet ./...`, and `go build ./...` passed, with all three dedicated PostgreSQL test DSNs enabled (eligibility/schema, generated-schema Service lifecycle/concurrency, and the existing canonical-reference lifecycle/replay test). Disposable server stopped after verification.
- Docker test target (`truckin-be:spp-service-test`) and production image build (`truckin-be:spp-service-local`) passed locally. Docker's default test target does not run opt-in PostgreSQL cases; those ran separately against the loopback disposable server.
- Generated Swagger updated from annotations. APP guide: `docs/mobile-spp-service-continuation-handover.md`; maintenance/migration/rollback guidance: `docs/spp-service-continuation-operations.md`.
- Confluence PRD `220495876` updated and read back as **Draf v1.20 / page version 26**.
- Apply migration **000006 before this BE** during a controlled writer pause. Downgrade refuses populated continuation audit links; never delete history or force a schema version to bypass refusal.

Existing unchecked tasks in `tasks/plan.md`/`tasks/todo.md` remain unchanged.
