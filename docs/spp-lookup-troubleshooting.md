# SPP Lookup Troubleshooting

## Confirmed Contract

Android calls only `GET /api/v1/movement-documents/spp?number=<URL-encoded-full-number>` on UnitLog. UnitLog calls the current SAOS V2 contract, `GET /api/v2/loading-orders?search=<URL-encoded-full-number>`, on the configured `saos_api_base_url`, with `Accept: application/json` and the configured `saos_service_token` as a Bearer token. Never forward the Android JWT or copy credentials into source code or logs.

The adapter trims the full number without splitting `/`, decodes the SAOS `data` array, and selects the exact matching `trans_no`. It maps `unit_name`, `unit_police_no`, `driver_name`, `customer_code`, and `customer_name`. A matching document must have a nonblank unit, police number, and trimmed driver name. Unrelated records and extra fields do not decide validity.

## Failure Diagnosis

| HTTP / code | Meaning | Next check |
| --- | --- | --- |
| 200 with `allowedOutDestinations: []` | Document is valid but no new SPP OUT is currently eligible | Keep IN lookup usable; check current status, cycle correlation, stable customer code, and lifecycle. Missing trusted legacy cycle data is not permission to infer an older SPP. |
| 422 `DOCUMENT_VALIDATION_FAILED` | No matching document, unsuccessful document response, or missing required document fields | Check the complete scanned number and the SAOS document data. |
| 404 `UNIT_NOT_FOUND` | Document unit does not resolve to an active Unit Lambung snapshot | Check Unit Lambung master data and the latest successful sync. |
| 422 `DOCUMENT_VALIDATION_FAILED`, with unit/police mismatch message | Document unit/police disagrees with the active snapshot | Compare SAOS and Unit Lambung; do not bypass validation or infer the unit from its plate. |
| 500 `INTERNAL_ERROR` | SAOS request/configuration, authentication/service, connection, or malformed response failed | Check the server's sanitized upstream failure reason and deployment configuration. |

Server logs distinguish `no_matching_trans_no`, `unsuccessful_response`, `missing_required_fields`, `invalid_response_envelope`, `missing_data_array`, `unit_not_found`, `unit_police_mismatch`, and `integration_unavailable`. Upstream HTTP failures include the status, not the body or authorization header. Driver/customer names and document payloads are not logged.

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

Regression tests cover the supplied SAOS array fixture, exact matching after an unrelated record, slash-containing numbers, trimming, empty/no-match/invalid data, upstream failures, active-unit lookup, police mismatch, safe diagnostics, and submit-time revalidation. Lookup tests permit only the unit SELECT and no movement, cycle, duration, or status writes. Runtime lookup may refresh master-unit cache data through the existing Unit Lambung refresher; it never records a movement.

The supplied fixture is accepted in automated tests when paired with the matching active snapshot. This does **not** verify a deployed lookup: the deployment's configuration, SAOS access, and snapshot must still be checked. No live configuration or snapshot data is changed by this fix.

The SPP success response adds the non-null `allowedOutDestinations` array; active-SPP/customer-code validation is now tied to the current IN's trusted departure cycle. App permission, movement request payloads, coordinates, returning-IN rules, durations, and idempotent replay behavior are unchanged. See [the APP eligibility handover](mobile-spp-out-eligibility-handover.md).
