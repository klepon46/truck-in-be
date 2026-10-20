# APP Agent Handover: SPP OUT Destination Eligibility

## Scope And Endpoint

Keep using `GET /api/v1/movement-documents/spp?number=<URL-encoded-full-SPP>` with `unitlog:app:default:view`. Android must not call web-only unit/history APIs or request broader permissions to decide OUT destinations.

The success response adds **one response-only field**, `allowedOutDestinations`. WO lookup, movement request payloads, optional coordinates, and idempotency hashes are unchanged. Do not include this field in `POST /api/v1/movements`.

```json
{
  "data": {
    "noLambung": "SAMT119",
    "noPolisi": "DA 1234 AB",
    "driverName": "Driver Name",
    "sppNumber": "SPP-001",
    "customerId": "C400",
    "customerName": "Customer Name",
    "allowedOutDestinations": ["CUSTOMER"]
  }
}
```

The array is always present for a successful SPP lookup. Its only possible entries are `CUSTOMER` and `FILLING_SHED_KUIN`; an ineligible SPP returns `[]`, never null or an omitted field.

## Eligibility

| Current movement state | Allowed SPP OUT destinations |
| --- | --- |
| IN Gantungan, scanned SPP and stable customer code match the trusted departure, and SPP is not completed | `["CUSTOMER"]` |
| IN Gantungan, but SPP/customer does not match or trusted cycle correlation is missing | `[]` |
| IN Kosongan or Service, SPP unused for Filling Shed and not completed | `["FILLING_SHED_KUIN"]` |
| No movement history, SPP unused/uncompleted | `["FILLING_SHED_KUIN"]` |
| Current OUT | `[]` |
| Used/completed SPP not eligible for the requested new departure | `[]` |

BE reads current movement state through `unit_snapshot.current_transaction_id`. Active SPP/customer is taken from the OUT departure linked to that current Gantungan IN by its completed cycle, not a random historical OUT. Customer display-name changes do not cause a mismatch when the stable `customerId` code remains unchanged.

Legacy Gantungan data without trustworthy cycle correlation fails closed with `[]`. Do not guess its SPP from history or silently rewrite audit records; operations must review missing/corrupt legacy correlation separately.

## Critical: IN Must Remain Usable

This lookup is shared by IN and OUT. A valid SPP for a unit currently OUT returns normal document data with `allowedOutDestinations: []`, so the app can still populate its returning-IN form.

**Do not treat an empty OUT array as a failed document lookup, block IN because of it, or automatically switch the user's chosen direction.** Existing returning-IN document matching and category validation are still enforced on submit.

UnitLog records only pool-boundary crossings. A direct `Pool → Filling Shed → Customer → Pool` journey still records one Filling Shed OUT and the returning IN; there is no extra Customer OUT outside the pool.

## APP Implementation Rules

1. Add the array to the SPP response DTO, not the movement request DTO. Preserve a distinction between a missing field and a known empty array for compatibility checks.
2. After **Cek SPP**, populate document data as before. For a new SPP OUT, display only recognized destinations returned in the array; never infer eligibility from cached or historical status.
3. Clear a selected destination if it is absent from the new array. Invalidate stale lookup/selection state when the document or unit changes; do not let a late response from an older lookup overwrite the current form.
4. If the array is empty, clear the OUT destination and disable confirmation for a **new OUT**. Explain that the unit/SPP is not currently eligible, without claiming a specific mismatch reason that the response does not provide.
5. If the field is missing, null, or malformed, eligibility is unknown: do not default to both destinations. Require a compatible BE/fresh lookup for a new SPP OUT. This does not create an additional IN restriction.
6. Lookup is not a reservation. Another satpam may change state before submit. Handle `422 INVALID_TRANSITION`, `422 DOCUMENT_VALIDATION_FAILED`, or `409 SPP_USED`; preserve the form, clear invalid choices, and offer a fresh lookup for a new attempt.
7. Preserve pending retries separately: resend their original payload and idempotency key, including the original optional coordinates. **Do not block a retry of an already-sent request merely because a later lookup returns `[]`**; an earlier successful submission may itself have changed the status to OUT. Never rebuild the pending payload from the lookup response or add the derived array.

## Errors And Read-Only Behavior

OUT ineligibility alone is `200` with normal document data and `[]`. Genuinely invalid documents, unknown units, unit/police mismatches, and integration errors retain their documented error responses. Database read failures remain server errors, not successful empty arrays.

Lookup adds no movement, current-status-pointer, document reservation, cycle, or duration writes. The existing Unit Lambung master-cache refresh for an unknown unit is retained; it does not record a movement.

## Deployment And Verification

- Deploy the updated BE **before** releasing mobile destination filtering. No new database migration or permission is required by eligibility; the existing application migrations must already be applied.
- Verify Gantungan offers only Customer, Kosongan/Service offers only Filling Shed for unused SPP, current OUT still allows an IN lookup, and missing/empty arrays are handled differently.
- Verify changing SPP clears an invalid old destination; a stale response cannot overwrite a newer lookup.
- Verify status changes after lookup are handled from submit errors and ambiguous network retries retain the original request/key.
- No live test movements should be created without explicit approval. BE query tests use isolated PostgreSQL transactions and roll back every fixture.

## References

- [Document/picker movement handover](mobile-document-movement-handover.md)
- [Optional coordinate handover](mobile-movement-coordinates-handover.md)
- [Movement cycles](mobile-movement-cycle-handover.md)
- [Frontend API guide](frontend-handover.md)
- Generated API definition: `/swagger-ui/doc.json` (`movement.SPPLookupDocument`).
