# SPP Lookup Troubleshooting

## Confirmed Contract

Android calls only `GET /api/v1/movement-documents/spp?number=<URL-encoded-id-owner>` on UnitLog. For `123-example`, UnitLog calls `GET /api/v2/loading-orders?id=123&owner=example` on `saos_api_base_url`, with `Accept: application/json` and `saos_service_token` as a Bearer token. The old `search` query is no longer used. Never forward the Android JWT or copy credentials into source code or logs.

The adapter splits at the first hyphen, trims ID/owner, and URL-encodes each component independently, preserving owner case and further hyphens. A consistent `data[]` projection resolves to its canonical nonblank `trans_no`; it is not compared to the input reference. Unit, police, and driver are required; customer fields map as before. Duplicate identical projections are accepted; contradictory rows fail closed. Lookup echoes the reference, while eligibility, returning-document matching, persistence, and history use canonical `trans_no`. Extra quantity/capacity fields do not select a row.

## Failure Diagnosis

| HTTP / code | Meaning | Next check |
| --- | --- | --- |
| 200 with `allowedOutDestinations: []` | Document is valid but no new SPP OUT is currently eligible | Keep IN lookup usable; check current status, cycle correlation, stable customer code, and lifecycle. Missing trusted legacy cycle data is not permission to infer an older SPP. |
| 400 `INVALID_DOCUMENT_QUERY` / submit 422 `VALIDATION_ERROR` | SPP reference has no delimiter, blank ID, or blank owner | Re-scan the complete `{id}-{owner}`; BE makes no SAOS call. |
| 404 `SPP_NOT_FOUND` | Successful ID/owner query returned an empty array or explicit null | Check the complete reference; this means unavailable in this lookup, not proof of deletion from every upstream table. |
| 422 `DOCUMENT_VALIDATION_FAILED` | Unsuccessful document response or missing required fields on the matching document | Check the SAOS document data; this is not confirmed absence. |
| 404 `UNIT_NOT_FOUND` | Document unit does not resolve to an active Unit Lambung snapshot | Check Unit Lambung master data and the latest successful sync. |
| 422 `DOCUMENT_VALIDATION_FAILED`, with unit/police mismatch message | Document unit/police disagrees with the active snapshot | Compare SAOS and Unit Lambung; do not bypass validation or infer the unit from its plate. |
| 500 `INTERNAL_ERROR` | SAOS request/configuration, authentication/service, connection, or malformed response failed | Check the server's sanitized upstream failure reason and deployment configuration. |

Server logs distinguish `empty_result`, `unsuccessful_response`, `missing_required_fields`, `ambiguous_reference_result`, `invalid_response_envelope`, `missing_data_array`, `invalid_document_query`, `unit_not_found`, `unit_police_mismatch`, and `integration_unavailable`. Upstream HTTP failures include the status, not the body or authorization header. ID/owner, driver/customer names, and document payloads are not logged.

The lookup-level absence reason is now `spp_not_found` (`wo_not_found` for Workshop). SAOS V2's handler returns a successful list and its repository can return a nil slice with no rows, serialized as explicit `data:null`; treat that as an empty result. An omitted `data` field or wrong data type remains an integration failure. SAOS HTTP 404 is a route/service failure, not its search-result absence contract. Workshop absence requires its documented HTTP 404 + `success:false` + `code:404` + `message:"Work order not found"` without a document; generic proxy/route 404 pages are not classified as document absence. See [the APP handover](mobile-document-not-found-handover.md).

## Snapshot Check For The Reported Document

Use a read-only database query on the same environment as the UnitLog process:

```sql
SELECT no_lambung, no_polisi, is_active, synced_at
FROM unit_snapshot
WHERE no_lambung = 'SAMT072';
```

The supplied SAOS record expects police number `DA 8729 CB`. Existing normalization ignores police-number case and leading/trailing whitespace, but does not remove internal spaces or otherwise fuzzy-match identifiers.

`EnsureKnown` can refresh an unknown/inactive unit from Unit Lambung. If an active snapshot already exists, it is used without an immediate refresh; investigate a stale plate using the authoritative master and sync history. Do not modify immutable movement snapshots or overwrite the master merely to make one lookup pass.

## Verification And Limits

Regression tests cover ID/owner splitting/encoding/case, the supplied SAOS array fixture, canonical identity, repeated/ambiguous projections, trimming, empty/invalid data, upstream failures, active-unit lookup, police mismatch, safe diagnostics, and submit-time revalidation. Lookup tests permit only the unit SELECT and no movement, cycle, duration, or status writes. Runtime lookup may refresh master-unit cache data through the existing Unit Lambung refresher; it never records a movement. A disposable PostgreSQL test verifies reference-based returns against old canonical cycles, alias reuse/completion guards, and original-payload replay/hash/coordinate behavior.

The supplied fixture is accepted in automated tests when paired with the matching active snapshot. This does **not** verify a deployed lookup: the deployment's configuration, SAOS access, and snapshot must still be checked. No live configuration or snapshot data is changed by this fix.

The SPP success response adds the non-null `allowedOutDestinations` array; active-SPP/customer-code validation is now tied to the current IN's trusted departure cycle. App permission, movement request payloads, coordinates, returning-IN rules, durations, and idempotent replay behavior are unchanged. See [the APP eligibility handover](mobile-spp-out-eligibility-handover.md).
