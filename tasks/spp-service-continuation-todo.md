# SPP Service Continuation Checklist

- [x] S1 — Add failing schema/SQL tests; implement migration 000006 with derived resumption references, normal/exception uniqueness, shared return/completion facts, and fail-closed downgrade. Verified disposable PostgreSQL: forged markers/identities, normal/source uniqueness, immutable history/cycles, empty downgrade and populated-link refusal.
- [x] S2 — Add failing Service continuation policy/transition/HTTP regressions, then align lookup and submit; retain fresh-SPP Filling Shed, Gantungan Customer-only, terminal/reuse and invalid-source rejection. Canonical-SPP lock precedes fresh IN/OUT validation; policy mutation was caught by tests. Full Go tests/vet/build passed.
- [ ] S3 — Prove repeated Service continuation, original-reference replay/coordinates, concurrent submits, terminal completion, historical immutability and migration rollback against real disposable PostgreSQL.
- [ ] S4 — Update Swagger, APP/operations guides and PRD; complete adversarial review and Go tests/vet/build/Docker verification. No live changes, pushes, or deployment.

Existing unchecked tasks in `tasks/plan.md`/`tasks/todo.md` remain unchanged.
