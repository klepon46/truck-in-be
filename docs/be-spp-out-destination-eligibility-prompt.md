# BE Agent Prompt: SPP OUT Destination Eligibility

Extend UnitLog's SPP lookup so Android can restrict OUT destinations based on authoritative backend eligibility.

## Objective

Prevent satpam from choosing:

- Customer unless the unit is currently **IN - Gantungan**.
- Filling Shed Kuin when the unit is currently **IN - Gantungan**.

Use the unit's **current movement status**, not an arbitrary historical IN or client-provided status.

## Endpoint

Extend the existing app-authorized endpoint:

```http
GET /api/v1/movement-documents/spp?number=<URL-encoded-SPP>
```

Keep the existing satpam permission: `unitlog:app:default:view`.

Do not require Android to call web-only unit/history endpoints or broaden its permissions.

## Proposed response addition

Add `allowedOutDestinations` as a JSON array of enum strings. For SPP, possible values are only `CUSTOMER` and `FILLING_SHED_KUIN`.

Example:

```json
{
  "data": {
    "noLambung": "SAMT072",
    "noPolisi": "DA 8729 CB",
    "driverName": "Driver Name",
    "sppNumber": "SPP-001",
    "customerId": "C400",
    "customerName": "Customer Name",
    "allowedOutDestinations": ["CUSTOMER"]
  }
}
```

SPP responses must always include this array, including `[]` when no SPP-backed OUT is permitted. Do not serialize an empty result as `null` or omit it.

## Eligibility rules

Provided the scanned SPP also satisfies existing document/lifecycle rules:

1. **Current status IN - WAITING_FOR_DELIVERY_TO_CUSTOMER:**
   - `CUSTOMER` only.
   - The scanned SPP must match the active SPP.
   - Validate the stable SAOS customer code against the active SPP customer.
   - Never allow `FILLING_SHED_KUIN`.
   - A valid document that does not qualify for this departure gets `[]`.

2. **Current status IN - WAITING_FOR_ASSIGNMENT or IN - SERVICE_AND_REPAIR_MAINTENANCE:**
   - `FILLING_SHED_KUIN` only.
   - Never allow `CUSTOMER`.

3. **Current status OUT:**
   - Return `[]`.

4. **No movement history:**
   - Preserve the existing rule permitting an otherwise-valid Filling Shed departure.
   - Never allow `CUSTOMER`.

Do not permit a completed/otherwise-ineligible SPP just because the status permits a particular destination.

## Critical: lookup is shared by IN and OUT

Android also uses this endpoint before returning IN.

OUT ineligibility must **not**, by itself, reject a valid document lookup. For example, a unit currently OUT must still be able to look up its departure SPP for returning IN; the response can contain normal document data plus `allowedOutDestinations: []`.

Preserve existing errors for genuinely invalid documents or unit/police mismatches. Do not confuse those errors with OUT eligibility.

## Implementation constraints

- Inspect and reuse existing movement eligibility/domain rules where possible; avoid duplicating divergent rules in the HTTP handler.
- Calculate eligibility from backend state, including active-SPP/customer rules.
- Lookup remains read-only: no transaction, status, document reservation, cycle, or duration mutation.
- No new database columns should be necessary for this derived response field.
- Preserve the WO lookup contract and existing movement request payloads.
- Clients must not submit `allowedOutDestinations` as movement input.
- Keep optional coordinates and existing idempotency behavior unchanged.
- `POST /api/v1/movements` must still atomically revalidate eligibility. Lookup is not a reservation; another satpam can change the state afterward.
- Do not log credentials, raw upstream responses, or unnecessary personal/location data.

## Regression tests

Cover:

- Gantungan + matching active SPP/customer returns `["CUSTOMER"]`.
- Gantungan + different SPP or mismatched customer returns `[]` when the document itself is valid.
- Customer-name changes with an unchanged stable customer code do not cause a mismatch.
- Kosongan/Service + otherwise-valid SPP returns `["FILLING_SHED_KUIN"]`.
- Current OUT returns `[]`, while returning-IN lookup remains usable.
- No history follows existing Filling Shed eligibility.
- Completed/ineligible SPP cannot gain eligibility.
- Empty eligibility serializes as `[]`, not `null` or omitted.
- Lookup causes no movement side effects.
- Submit still rejects state changes occurring after lookup.
- Existing idempotent replays remain valid.
- Existing authentication, document validation, and coordinate tests remain passing.

## Documentation and delivery

Update generated Swagger and the relevant frontend/mobile handovers.

Document that:

- Android should show only destinations returned by BE.
- A missing field means eligibility is unknown, not that both destinations are allowed.
- Invalid previous selections must be cleared after a new lookup.
- BE must be deployed before the mobile filtering update is released.

Finish with the implemented contract, changed files, tests/results, and any deployment requirements.

Do not create live test movements without explicit approval.
