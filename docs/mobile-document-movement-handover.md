# Mobile Document Movement Handover

Use this guide for the satpam movement flow. The app no longer scans a unit QR. It calls UnitLog only; UnitLog validates all Workshop, SAOS, Unit Lambung, and Driver data server-side.

## Flow

1. Satpam chooses `IN` or `OUT`.
2. Satpam chooses **WO**, **SPP**, or **OTHER**.
3. For WO/SPP, scan the document QR and call the matching UnitLog lookup endpoint. For OTHER, choose a unit and active driver from UnitLog pickers, then enter a note.
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
GET /api/v1/movement-documents/spp?number=<url-encoded-spp>
Authorization: Bearer <satpam-access-token>
```

Both endpoints return the fields needed to populate the form:

```json
{
  "data": {
    "noLambung": "A122",
    "noPolisi": "DA 122 A",
    "driverName": "Driver Name",
    "customerId": "CUST-1",
    "workOrderNumber": "WO-001"
  }
}
```

An SPP lookup returns `sppNumber` instead of `workOrderNumber`, plus the SAOS `customerId` (stable customer code) and `customerName` when supplied. Do not allow the satpam to edit document-populated unit, police, driver, or customer values.

## OTHER Pickers

```http
GET /api/v1/unit-picker?query=<optional-unit-or-police-search>&page=1&pageSize=25
GET /api/v1/drivers?name=<url-encoded-driver-search>
Authorization: Bearer <satpam-access-token>
```

Use the selected unit's `noLambung` and display `noPolisi`. Use the selected driver's `id` and `driverName`. `note` remains manually entered and is required.

## Submit Payloads

`noLambung` is populated by the lookup or picker. It is not scanned from a unit QR.

### Document-backed OUT

```json
{
  "noLambung": "A122",
  "direction": "OUT",
  "outDestination": "BENGKEL_LUAR",
  "workOrderNumber": "WO-001"
}
```

For `FILLING_SHED_KUIN` and `CUSTOMER`, submit `sppNumber` instead. Existing status rules still determine whether CUSTOMER is allowed.

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
- `404 UNIT_NOT_FOUND`: document vehicle code does not resolve to an active Unit Lambung snapshot.
- `422 DOCUMENT_VALIDATION_FAILED`: document data is invalid, conflicts with its resolved unit/police data, or an IN document does not match the open OUT.
- `422 INVALID_TRANSITION`: another movement changed the unit state before submit.

## Client Rules

1. Never call Workshop, SAOS, Unit Lambung, or Driver directly and never store service tokens.
2. Lookup/picker values are display state only; submit the minimal source fields shown above.
3. Reuse an `Idempotency-Key` only to retry the identical POST body.
4. A successful IN/OUT stores the confirmed unit, police, driver, document/OTHER note, actor, cycle, and duration audit data.

## References

- [Frontend handover](frontend-handover.md)
- [Movement cycle handover](mobile-movement-cycle-handover.md)
- Generated API definition: `http://172.16.17.17:8105/swagger-ui/index.html`
