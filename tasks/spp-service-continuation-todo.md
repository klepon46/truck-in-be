# SPP Service Continuation Checklist

- [ ] S1 — Add failing schema/SQL tests; implement migration 000006 with derived resumption references, normal/exception uniqueness, shared return/completion facts, and fail-closed downgrade. Verify disposable PostgreSQL only.
- [ ] S2 — Add failing Service continuation policy/transition/HTTP regressions, then align lookup and submit; retain fresh-SPP Filling Shed, Gantungan Customer-only, terminal/reuse and invalid-source rejection.
- [ ] S3 — Prove repeated Service continuation, original-reference replay/coordinates, concurrent submits, terminal completion, historical immutability and migration rollback against real disposable PostgreSQL.
- [ ] S4 — Update Swagger, APP/operations guides and PRD; complete adversarial review and Go tests/vet/build/Docker verification. No live changes, pushes, or deployment.

Existing unchecked tasks in `tasks/plan.md`/`tasks/todo.md` remain unchanged.
