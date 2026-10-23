# Mobile Document Movement Handover

Use this guide for the satpam movement flow. The app no longer scans a unit QR. It calls UnitLog only; UnitLog validates all Workshop, SAOS, Unit Lambung, and Driver data server-side.

## Flow

1. Satpam chooses `IN` or `OUT`.
2. Satpam chooses **WO**, **SPP**, or **OTHER**.
3. WO QR contains the raw WO number; SPP QR contains an `{id}-{owner}` reference such as `123-example` (neither is a URL). After scanning, put that complete value into the document-number field; the satpam then presses **Cek WO** or **Cek SPP** to call the matching UnitLog lookup endpoint. For OTHER, choose a unit and active driver from UnitLog pickers, then enter a note.
4. Display the populated unit code, police number, and driver. Satpam chooses the IN category or OUT destination as applicable, then confirms.
5. Submit `POST /api/v1/movements` with a new idempotency key. UnitLog revalidates all selected/scanned data before changing status.

Document lookup and picker calls are read-only: they never create a transaction, change a unit status, start/close a cycle, or finalize a duration.

## WO Lookup

```http
GET /api/v1/movement-documents/work-orders?number=<url-encoded-wo>
Authorization: Bearer <satpam-access-token>
```

## SPP Lookup

```http
GET /api/v1/movement-documents/spp?number=<url-encoded-id-owner-reference>
Authorization: Bearer <satpam-access-token>
```

Both endpoints return the fields needed to populate the form:

```json
{
  "data": {
    "noLambung": "A122",
    "noPolisi": "DA 122 A",
    "driverName": "Driver Name",
    "workOrderNumber": "WO-001"
  }
}
```

An SPP lookup returns `sppNumber` instead of `workOrderNumber`, plus the SAOS `customerId` (stable customer code) and `customerName` when supplied. Do not allow the satpam to edit document-populated unit, police, driver, or customer values.

```json
{
  "data": {
    "noLambung": "SAMT072",
    "noPolisi": "DA 8729 CB",
    "driverName": "Driver Name",
    "sppNumber": "123-example",
    "customerId": "C400",
    "customerName": "Customer Name",
    "allowedOutDestinations": ["FILLING_SHED_KUIN"]
  }
}
```

UnitLog splits the reference on its first hyphen, trims each part, and calls SAOS with separate `id`/`owner` query parameters. Owner case and remaining hyphens are preserved. A consistent SAOS `data[]` resolves to its canonical `trans_no`; identical projections may repeat, conflicting rows are rejected. Eligibility and audit use the canonical number, while lookup `sppNumber` echoes the trimmed reference for APP submit. `customerId` comes from `customer_code`; unit/police still must match the active snapshot. Extra quantity/capacity/`is_marked` fields do not select a document. See [the ID/owner reference handover](mobile-spp-id-owner-handover.md).

For a **new SPP OUT**, show only `allowedOutDestinations` returned by BE. This field is always an array, including `[]` for a valid document with no eligible OUT. A missing field means eligibility is unknown, not that both destinations are allowed. Clear an old destination if it is not present after a new lookup. The array does not restrict IN or authorize changes to a pending retry; see [the SPP eligibility APP handover](mobile-spp-out-eligibility-handover.md).

## OTHER Pickers

```http
GET /api/v1/unit-picker?query=<optional-unit-or-police-search>&page=1&pageSize=25
GET /api/v1/drivers?name=<url-encoded-driver-search>
Authorization: Bearer <satpam-access-token>
```

Use the selected unit's `noLambung` and display `noPolisi`. Use the selected driver's `id` and `driverName`. `note` remains manually entered and is required.

OTHER driver revalidation at submit is enabled by default. The server-only testing flag `validate_other_driver_on_submit=false` skips the extra Driver call for OTHER IN/OUT, without changing pickers, required positive driver ID/name/note, or other movement checks. APP payloads stay unchanged; snapshots are then APP-reported rather than verified active at submit. See [the OTHER validation handover](mobile-other-driver-validation-handover.md).

## Submit Payloads

`noLambung` is populated by the lookup or picker. It is not scanned from a unit QR.

Every IN/OUT payload may also include one optional string, `coordinates: "-3.319437,114.590889"`, in latitude/longitude order. Missing GPS must not block submission. Keep coordinates fixed with the original idempotency key/payload on retries. See [the APP coordinate handover](mobile-movement-coordinates-handover.md) for the complete format, null handling, errors, and deployment order.

### Document-backed OUT

```json
{
  "noLambung": "A122",
  "direction": "OUT",
  "outDestination": "BENGKEL_LUAR",
  "workOrderNumber": "WO-001"
}
```

For `FILLING_SHED_KUIN` and `CUSTOMER`, submit `sppNumber: "123-example"` instead. Use the same reference format for SPP-backed IN. Movement/history responses contain canonical `sppNumber`, not the reference; never use them to rebuild a request. Existing status rules still determine whether CUSTOMER is allowed.

### Filling Shed To Customer Routes

UnitLog records only crossings of the pool boundary; it does not record movements between external destinations.

- **Direct route:** `Pool → Filling Shed → Customer → Pool` records `OUT FILLING_SHED_KUIN` when leaving the pool, then records the returning `IN` with the same SPP when the unit returns. Do not create a synthetic IN/OUT transaction for Filling Shed → Customer.
- **Return as Gantungan:** `Pool → Filling Shed → Pool → Customer → Pool` records `OUT FILLING_SHED_KUIN`, then `IN WAITING_FOR_DELIVERY_TO_CUSTOMER` with the same SPP. The unit can then submit `OUT CUSTOMER` with that same SPP; when it returns, submit `IN` with the Customer departure's same SPP.

`OUT CUSTOMER` is therefore only for a departure **from the pool** while the unit status is `IN WAITING_FOR_DELIVERY_TO_CUSTOMER`, with the same uncompleted SPP and stable customer code from that current IN's correlated departure cycle. Present it only when it is returned by `allowedOutDestinations`; UnitLog still revalidates submit and may reject stale eligibility with `422 INVALID_TRANSITION`, `422 DOCUMENT_VALIDATION_FAILED`, or `409 SPP_USED`.

### Returning document-backed IN

```json
{
  "noLambung": "A122",
  "direction": "IN",
  "inCategory": "WAITING_FOR_ASSIGNMENT",
  "workOrderNumber": "WO-001"
}
```

A returning IN must scan and submit the **same document number and source type** as the unit's open OUT. An OUT with WO requires the same WO; an OUT with SPP requires the same SPP. A different WO/SPP, or OTHER used to bypass a document departure, receives `422 DOCUMENT_VALIDATION_FAILED`.

For SPP, this equality is checked on the canonical `trans_no` resolved from the reference. An existing pre-cutover SPP OUT can return using a new-format reference resolving to the same document; do not rewrite history or guess the reference from a displayed canonical number.

### OTHER in Either Direction

```json
{
  "noLambung": "A122",
  "direction": "IN",
  "inCategory": "WAITING_FOR_ASSIGNMENT",
  "driverId": 42,
  "driverName": "Driver Name",
  "note": "Arrival note"
}
```

For OUT, replace `inCategory` with `outDestination: "OTHER"`. OTHER must use both pickers and a non-empty note.

## Error Handling

- `400 INVALID_DOCUMENT_QUERY`: QR/document number is empty or malformed for lookup.
- `404 WORK_ORDER_NOT_FOUND`: `WO tidak ditemukan di Workshop.` Confirmed by the Workshop response; applies to lookup and fresh IN/OUT submit.
- `404 SPP_NOT_FOUND`: `SPP tidak ditemukan di SAOS.` SAOS returned a successful empty/null result for ID/owner; applies to lookup and fresh IN/OUT submit.
- `422 VALIDATION_ERROR`: a fresh submit SPP reference is malformed (missing delimiter/ID/owner).
- `404 UNIT_NOT_FOUND`: document vehicle code does not resolve to an active Unit Lambung snapshot.
- `422 DOCUMENT_VALIDATION_FAILED`: document data is invalid, conflicts with its resolved unit/police data, or an IN document does not match the open OUT.
- `422 INVALID_TRANSITION`: another movement changed the unit state before submit.
- `500 INTERNAL_ERROR`: WO/SPP integration/configuration, upstream authentication/service, connection, or response-parsing failure. This is not document absence or a document-validation rejection. Do not tell the satpam to change the document number solely because of this response.

For a unit/police mismatch, the validation message asks operations to check master data; never override the resolved unit or bypass this validation. Keep the stable HTTP status/code as the app contract rather than matching error-message text. See [SPP lookup troubleshooting](spp-lookup-troubleshooting.md) for backend diagnostics.

For missing/null/blank SAOS `unit_name`, lookup and SPP IN/OUT submit use `422 DOCUMENT_VALIDATION_FAILED` with `Kode Unit Lambung tidak tersedia pada data SPP dari SAOS. Hubungi operasional untuk memeriksa data unit.` Display that safe message rather than replacing it with a generic error. It is not a missing-master `404 UNIT_NOT_FOUND`; validation remains strict.

The dedicated [WO/SPP not-found APP handover](mobile-document-not-found-handover.md) includes full error bodies, UI behavior, and pending retry rules. Do not interpret every 404 as a missing WO/SPP: `UNIT_NOT_FOUND` is a separate master-unit issue.

## Client Rules

1. Never call Workshop, SAOS, Unit Lambung, or Driver directly and never store service tokens.
2. Lookup/picker values are display state only; submit the minimal source fields shown above.
3. Persist the original POST payload and its `Idempotency-Key` before attempting submission. Reuse both only to retry that identical movement; never rebuild or migrate an older pending payload using newer app rules.
4. A successful IN/OUT stores the confirmed unit, police, driver, document/OTHER note, actor, cycle, and duration audit data.

## References

- [Frontend handover](frontend-handover.md)
- [Movement cycle handover](mobile-movement-cycle-handover.md)
- [Optional coordinates APP handover](mobile-movement-coordinates-handover.md)
- [SPP OUT eligibility APP handover](mobile-spp-out-eligibility-handover.md)
- Generated API definition: `http://172.16.17.17:8105/swagger-ui/index.html`
