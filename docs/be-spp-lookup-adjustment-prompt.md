# UnitLog BE: SPP Lookup Investigation and Adjustment Prompt

Copy the instructions below to the backend AI agent.

---

Investigate and fix the SPP document lookup integration in UnitLog BE.

## Observed issue

Android calls:

```http
GET /api/v1/movement-documents/spp?number=SPG-BJM-2612481
```

UnitLog responds:

```text
HTTP 422
DOCUMENT_VALIDATION_FAILED
movement document is invalid
```

However, requesting the same SPP directly from SAOS succeeds.

## Confirmed SAOS request contract

```http
GET http://172.16.17.17:8087/api/v1/loading-orders?transNo=SPG-BJM-2612481
Accept: application/json
Authorization: Bearer <configured-SAOS-service-token>
```

Use `saos_api_base_url` and `saos_service_token` from the existing backend configuration. Never hardcode credentials or forward the satpam JWT to SAOS.

Actual SAOS response:

```json
{
  "success": true,
  "message": "Success",
  "code": 200,
  "data": [
    {
      "trans_no": "SPG-BJM-2612481",
      "unit_police_no": "DA 8729 CB",
      "driver_name": "JOYO LUMBANRAJA ",
      "unit_capacity": "10000",
      "unit_name": "SAMT072",
      "customer_code": "C400",
      "customer_name": "PT KAPUAS MAJU JAYA",
      "qty": 10000,
      "is_marked": false
    }
  ]
}
```

## Investigation

1. Trace the exact failure leading to `DOCUMENT_VALIDATION_FAILED`. Do not assume the cause is response parsing.
2. Check that the configured SAOS URL, query parameter `transNo`, authentication, and `Accept` header match the successful request.
3. Check whether the adapter correctly decodes `data` as an **array** and selects the record whose `trans_no` matches the complete requested number—not simply the first record.
4. Check whether `SAMT072` resolves to an active unit snapshot and whether its police number matches `DA 8729 CB` under existing normalization rules.
5. Check required-field validation, including trimming `driver_name`.
6. Distinguish integration/parsing failures from genuine document or unit-data mismatches. Preserve the documented error semantics.

## Required behavior

- Preserve the Android-facing endpoint and response shape:

  ```http
  GET /api/v1/movement-documents/spp?number=<URL-encoded-number>
  ```

- Trim and URL-encode the complete document number. Never split numbers containing `/`.
- Normalize the matched SAOS record into:

  ```json
  {
    "data": {
      "noLambung": "SAMT072",
      "noPolisi": "DA 8729 CB",
      "driverName": "JOYO LUMBANRAJA",
      "sppNumber": "SPG-BJM-2612481",
      "customerId": "C400",
      "customerName": "PT KAPUAS MAJU JAYA"
    }
  }
  ```

- `customerId` comes from `customer_code`; do not compare customer names as identifiers.
- Ignore unrelated SAOS fields unless an existing documented rule requires them.
- Preserve unit/police validation. Do not bypass validation just to make this fixture succeed.
- Lookup must remain read-only: no movement, status change, cycle, or duration mutation.
- Ensure submit-time SPP revalidation uses the same corrected integration behavior while preserving transition, active-SPP, customer, and returning-IN rules.
- Android must continue calling UnitLog only.

## Diagnostics

Provide a safe, actionable client validation message where appropriate. Keep detailed internal failure reasons in server logs without logging tokens, authorization headers, raw upstream responses, or unnecessary personal data.

## Tests

Add focused regression tests covering:

- The actual array response above.
- A matching record appearing after a nonmatching record.
- Empty arrays and no exact `trans_no` match.
- Malformed responses and missing required fields.
- Whitespace trimming and document numbers containing `/`.
- Unit-not-found and police-number mismatch.
- Upstream authentication/service failures.
- Lookup having no movement side effects.

Follow existing project patterns and make the smallest necessary change. Update relevant handover documentation if the confirmed contract differs.

## Completion report

Finish with:

1. The verified root cause.
2. Files changed and behavior corrected.
3. Tests run and results.
4. Any configuration or snapshot-data correction still required.

Do not claim the live SPP lookup is fixed unless it has actually been verified. Do not invent or expose credentials.
