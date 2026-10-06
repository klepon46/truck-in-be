# UnitLog Frontend Handover

This guide is the frontend integration contract for UnitLog Pool Basirih. It is based on the implemented backend, generated OpenAPI definition, and [UnitLog PRD](https://sadp-team.atlassian.net/wiki/spaces/ST/pages/220495876/Unit+Log+TRY-In+Out+Basirih+Pool).

Use this document for Android satpam and web operations clients. The generated API contract remains available at `/swagger-ui/doc.json`.

## Service URLs

| Environment | Base URL | Swagger UI |
| --- | --- | --- |
| Development | `http://172.16.17.17:8105` | `http://172.16.17.17:8105/swagger-ui/index.html` |
| Production | Obtain from deployment configuration | `<base-url>/swagger-ui/index.html` |

All application endpoints are under `<base-url>/api/v1`. `GET <base-url>/health` is the only unauthenticated endpoint.

## Authentication And Permissions

UnitLog does not provide login, token refresh, user lookup, or role-management endpoints. Obtain an Auth Service HS256 JWT through the approved Auth Service flow, then send it on every `/api/v1` request:

```http
Authorization: Bearer <access-token>
```

| Client | Required permission | Permitted UnitLog API |
| --- | --- | --- |
| Android satpam | `unitlog:app:default:view` | `POST /api/v1/movements` |
| Web operations | `unitlog:web:default:view` | Dashboard, unit, transaction, and CSV routes |

The JWT must also contain non-empty configured actor ID and actor name claims for movement submission. UnitLog stores these as immutable transaction audit data.

Do not send Unit Lambung, Workshop, SAOS, Driver, Consul, or service tokens from the frontend. UnitLog calls those services internally.

## Shared API Rules

### JSON And Dates

- Send request bodies as `Content-Type: application/json`.
- All JSON property names use `camelCase`.
- `null` response values mean the field is not applicable or no transaction exists.
- Backend timestamps are RFC3339 UTC values. Convert them to the browser or device timezone only for display.
- `durationSeconds` is an integer duration calculated by the server. Format it in the client, for example as days, hours, and minutes.

### Response Shapes

One result:

```json
{ "data": {} }
```

Paginated result:

```json
{
  "data": [],
  "pagination": {
    "page": 1,
    "pageSize": 25,
    "totalItems": 0
  }
}
```

Calculate page count in the frontend with `Math.ceil(totalItems / pageSize)`. Pagination is 1-based. The default page size is `25`; the maximum is `100`.

Errors always use:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "movement input is invalid"
  }
}
```

Display the stable `code` for client behavior and the `message` as a safe user-facing fallback. Do not expose raw network errors or tokens in the UI.

### Browser Origin Constraint

UnitLog allows browser requests only from origins configured in Consul under `cors_allowed_origins`. Development allows `http://172.16.17.17:3022`.

The frontend must send the Bearer token in `Authorization`; the backend responds to browser preflight requests for `GET` and `POST` API calls. New frontend origins must be added to the backend allowlist before deployment.

## API Client Shape

Keep the base URL environment-specific and centralize authorization, JSON decoding, and error handling. This is TypeScript-style pseudocode; use the equivalent in the chosen frontend framework.

```ts
type ApiError = {
  error: {
    code: string;
    message: string;
  };
};

async function request<T>(
  path: string,
  token: string,
  init: RequestInit = {},
): Promise<T> {
  const response = await fetch(`${UNITLOG_API_BASE_URL}${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...init.headers,
    },
  });

  if (!response.ok) {
    throw (await response.json()) as ApiError;
  }
  return response.json() as Promise<T>;
}
```

Download CSV endpoints as a `Blob`, not JSON. Preserve the server `Content-Disposition` filename when the frontend platform supports it.

## Movement Submission For Android

### Endpoint

```http
POST /api/v1/movements
Authorization: Bearer <satpam-jwt>
Idempotency-Key: <uuid>
Content-Type: application/json
```

Generate one UUID when the satpam confirms a scan. Reuse the same UUID only when retrying that exact payload after a timeout or temporary network failure. Generate a new UUID for a new movement attempt.

Submitting the same UUID with different data returns `409 IDEMPOTENCY_CONFLICT`. Do not automatically retry a `4xx` response other than an intentional retry of the unchanged request after an ambiguous network failure.

### QR Handling

Extract the final non-empty path segment from the scanned QR URL as `noLambung`. Example: `https://example.invalid/SAMT223` becomes `SAMT223`.

Trim it before submitting. The backend uppercases and validates it. There is no public UnitLog lookup-by-QR endpoint; submitting the movement causes UnitLog to refresh an unknown unit from Unit Lambung when needed.

### Important: Submit Only One Valid Payload Variant

Swagger shows every optional property in one combined schema. That is not a valid movement body. `sppNumber` and `workOrderNumber` are mutually exclusive, and fields belonging to another destination must be omitted.

#### IN

Use one category. Do not send any OUT, driver, document, or note field.

```json
{
  "noLambung": "SAMT223",
  "direction": "IN",
  "inCategory": "WAITING_FOR_ASSIGNMENT"
}
```

Available values:

- `WAITING_FOR_ASSIGNMENT`
- `WAITING_FOR_DELIVERY_TO_CUSTOMER`
- `SERVICE_AND_REPAIR_MAINTENANCE`

#### OUT - Bengkel Luar

```json
{
  "noLambung": "SAMT223",
  "direction": "OUT",
  "outDestination": "BENGKEL_LUAR",
  "workOrderNumber": "WO-001"
}
```

`workOrderNumber` is required. Do not send `sppNumber`, `driverId`, or `driverName`. `note` is optional. UnitLog validates the work order with Workshop and stores the Workshop driver snapshot.

#### OUT - Filling Shed Kuin

```json
{
  "noLambung": "SAMT223",
  "direction": "OUT",
  "outDestination": "FILLING_SHED_KUIN",
  "sppNumber": "SPP-001"
}
```

`sppNumber` is required. Do not send `workOrderNumber`, `driverId`, or `driverName`. `note` is optional. UnitLog validates the SPP with SAOS and stores the SAOS driver snapshot.

#### OUT - Customer

```json
{
  "noLambung": "SAMT223",
  "direction": "OUT",
  "outDestination": "CUSTOMER",
  "sppNumber": "SPP-001"
}
```

This requires the unit's current status to be `IN - WAITING_FOR_DELIVERY_TO_CUSTOMER`. The SPP must match the unit's active SPP and the SAOS customer must match the active SPP customer. Do not send `workOrderNumber`, `driverId`, or `driverName`.

#### OUT - Other

```json
{
  "noLambung": "SAMT223",
  "direction": "OUT",
  "outDestination": "OTHER",
  "driverId": 42,
  "driverName": "Driver Name",
  "note": "Operational note"
}
```

`driverId`, `driverName`, and `note` are all required. Do not send `workOrderNumber` or `sppNumber`. UnitLog verifies that the selected driver is active through the Driver service.

### Movement Response

Success returns `201`:

```json
{
  "data": {
    "transactionId": 123,
    "lambungUnitId": 58,
    "noLambung": "SAMT223",
    "noPolisi": "DA 8857 CZ",
    "direction": "OUT",
    "outDestination": "BENGKEL_LUAR",
    "driverName": "Workshop Driver",
    "workOrderNumber": "WO-001",
    "occurredAt": "2026-10-01T08:00:00Z",
    "actorId": "satpam-1",
    "actorName": "Satpam Name"
  }
}
```

The server owns `occurredAt`, document-derived driver/customer fields, snapshots, and actor audit fields. Render the returned transaction as the source of truth.

### Movement Error Handling

| Status | Code | Frontend behavior |
| --- | --- | --- |
| 400 | `INVALID_IDEMPOTENCY_KEY` or `INVALID_REQUEST` | Correct request construction; do not retry automatically. |
| 401 | `UNAUTHORIZED` | Renew/login through Auth Service. |
| 403 | `FORBIDDEN` or `INVALID_ACTOR` | Block action and show an access/account issue. |
| 404 | `UNIT_NOT_FOUND` | Show that the scanned unit is unavailable. |
| 409 | `IDEMPOTENCY_CONFLICT`, `WORK_ORDER_USED`, `SPP_USED` | Keep form input visible; require user correction or a new movement. |
| 422 | `VALIDATION_ERROR`, `INVALID_TRANSITION`, `DOCUMENT_VALIDATION_FAILED` | Show a contextual form/status validation message. |
| 500 | `INTERNAL_ERROR` | Show a retryable service failure without exposing details. |

## Web Operations APIs

All web operations routes require `unitlog:web:default:view`.

### Dashboard

```http
GET /api/v1/dashboard
```

Response fields:

| Field | Meaning |
| --- | --- |
| `totalUnits` | Active units from the latest successful Unit Lambung sync. |
| `unitsIn` | Active units whose latest movement is `IN`. |
| `unitsOut` | Active units whose latest movement is `OUT`. |
| `unitsNoStatus` | Active units with no UnitLog movement. |

Use this response for summary cards. Do not derive totals from only the current page of the units endpoint.

### List Units And Monitoring

```http
GET /api/v1/units?query=&status=&destination=&includeInactive=&page=&pageSize=
```

| Query | Values | Behavior |
| --- | --- | --- |
| `query` | text | Searches `noLambung` and `noPolisi`. |
| `status` | `IN`, `OUT`, `NULL` | Filters current movement direction or units without a movement. |
| `destination` | `BENGKEL_LUAR`, `FILLING_SHED_KUIN`, `CUSTOMER`, `OTHER` | Filters current OUT destination. |
| `includeInactive` | `true` or omitted | Defaults to active units only; only literal `true` includes inactive units. |
| `page` | integer >= 1 | Defaults to `1`. |
| `pageSize` | integer 1-100 | Defaults to `25`. |

Render these unit fields:

| Field | UI guidance |
| --- | --- |
| `id` | Use for detail and history routes. |
| `noLambung`, `noPolisi` | Primary unit identifiers. |
| `isActive` | Show inactive state only when requested. |
| `status` | `NULL`, `OUT`, or `IN - <inCategory>`. Use this display-ready field. |
| `outDestination`, `driverName` | May be `null`; show a placeholder. |
| `lastUpdatedAt` | May be `null`; convert from UTC for display. |
| `durationSeconds` | Format client-side. |

### Unit Detail

```http
GET /api/v1/units/{id}
```

Use the same unit fields as the list response. A non-positive or non-numeric `id` returns `400 INVALID_UNIT_ID`; an unknown ID returns `404 UNIT_NOT_FOUND`.

### Unit History

```http
GET /api/v1/units/{id}/transactions?page=&pageSize=
```

Transactions are newest first. Use the same pagination controls as the unit list. Each result includes `transactionId`, unit snapshots, direction, IN category or OUT destination, driver, WO/SPP, customer, note, actor name, and `occurredAt`.

### Transaction History

```http
GET /api/v1/transactions?query=&direction=&detail=&from=&to=&page=&pageSize=
```

| Query | Values | Behavior |
| --- | --- | --- |
| `query` | text | Searches transaction unit snapshot code and plate. |
| `direction` | `IN`, `OUT` | Filters movement direction. |
| `detail` | IN category or OUT destination enum | Filters the applicable movement detail. |
| `from` | RFC3339 timestamp | Inclusive UTC lower bound. |
| `to` | RFC3339 timestamp | Exclusive UTC upper bound. |
| `page`, `pageSize` | as above | 1-based pagination; maximum size 100. |

Invalid `from` or `to` returns `400 INVALID_FILTER`. Send complete RFC3339 values, for example `2026-10-01T00:00:00Z`, rather than a browser-local date string.

### CSV Exports

```http
GET /api/v1/units/export?query=&status=&destination=&includeInactive=
GET /api/v1/transactions/export?query=&direction=&detail=&from=&to=
```

Both routes return `text/csv` with an attachment filename. Pass exactly the same relevant filter values as their list screen.

Current implementation limitation: each CSV export fetches at most the first 100 matching rows and does not return pagination metadata. Do not label it as a complete all-record export. Request a backend export improvement if the operations team requires full datasets.

## Frontend Gaps Requiring A Decision

### Active Driver Search For OUT - Other

The PRD requires satpam to search and select an active driver. UnitLog currently validates a submitted `driverId` and `driverName`, but does not expose a public driver-search endpoint.

Do not call the Driver service directly from Android or web clients and do not expose the service token. Before implementing the `OTHER` driver picker, obtain one approved solution:

1. Add a UnitLog endpoint that proxies active-driver search with satpam authorization.
2. Use an existing authenticated backend endpoint approved by the Driver service owner.
3. Supply driver ID and name from an approved client-owned source.

### Optional Prevalidation

UnitLog has no public endpoints to prevalidate a work order, SPP, or QR unit. Submit the selected movement and render the server validation result. The backend is the authority for document validity and transition state.

## Screen Delivery Checklist

### Android Satpam

- Obtain and refresh the Auth Service token outside UnitLog.
- Parse the scanned QR to `noLambung`.
- Render mutually exclusive IN/OUT form controls.
- Generate an idempotency UUID once per confirmed submit.
- Disable duplicate submission while the request is in flight.
- On an ambiguous network failure, offer retry with the unchanged payload and same key.
- Render server transaction time and document-derived driver data from the response.
- Do not expose internal integration errors or tokens.

### Web Operations

- Use dashboard totals for summary cards.
- Default unit monitoring to active units only.
- Persist filters and 1-based pagination in route query state where appropriate.
- Render null fields as an explicit empty state, not as `undefined` or a fabricated value.
- Convert UTC timestamps to browser-local display time.
- Download CSV as a file and disclose the 100-row export limit.
- Use unit `id` for detail and transaction-history navigation.

## Reference Files

- `docs/swagger.yaml`: generated OpenAPI 2.0 contract.
- `docs/swagger.json`: generated JSON OpenAPI contract.
- `internal/httpapi/router.go`: route, validation, status-code, and CSV behavior.
- `internal/movement/validation.go`: exact mutually exclusive movement input rules.
- `internal/operations/service.go`: filtering, pagination, status, and export behavior.
- `docs/lambung-unit-snapshot-sync.md`: Unit Lambung producer contract; frontend clients do not call it directly.
