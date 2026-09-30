# Lambung Unit Snapshot Sync API

## Status

Proposed contract for UnitLog synchronization.

## Purpose

UnitLog periodically synchronizes the Lambung units eligible for operational use. This endpoint is the authoritative source for that synchronization. It returns only the fields UnitLog needs and omits units that are not found in SAP.

## Endpoint

```http
GET /lambung/v1/internal/unit-snapshots
Authorization: Bearer <service-token>
```

The endpoint is internal. It must require a service token with permission to read the UnitLog synchronization projection.

## Query Parameters

| Name | Required | Rules |
| --- | --- | --- |
| `cursor` | No | Opaque cursor returned by the previous response. Omit it for the first page. |
| `limit` | No | Maximum items to return. Default: `100`; minimum: `1`; maximum: `200`. |

## Eligibility Rules

Return a unit only when all conditions are true:

- `isSapFound` is `true`.
- `id` is a positive integer.
- `noLambung` is non-empty after trimming whitespace.
- `noPolisi` is non-empty after trimming whitespace.

Do not return units where `isSapFound` is `false`. UnitLog treats a successfully completed synchronization as authoritative and marks previously synchronized units that are absent from the full result set as inactive.

## Success Response

```json
{
  "message": "success",
  "success": true,
  "code": 200,
  "data": {
    "items": [
      {
        "id": 58,
        "noLambung": "SAMT027",
        "noPolisi": "DA 8857 CZ"
      }
    ],
    "nextCursor": "eyJsYXN0SWQiOjU4fQ",
    "hasMore": true
  }
}
```

The final page must set `hasMore` to `false` and `nextCursor` to `null`.

```json
{
  "message": "success",
  "success": true,
  "code": 200,
  "data": {
    "items": [],
    "nextCursor": null,
    "hasMore": false
  }
}
```

## Response Field Contract

| Field | Type | Rules |
| --- | --- | --- |
| `data.items` | array | At most `limit` eligible units, sorted by `id` ascending. |
| `data.items[].id` | integer | Positive immutable Lambung unit ID. |
| `data.items[].noLambung` | string | Trimmed, non-empty unit number. |
| `data.items[].noPolisi` | string | Trimmed, non-empty vehicle registration number. |
| `data.nextCursor` | string or null | Cursor for the next page; null only on the final page. |
| `data.hasMore` | boolean | True only when another page is available. |

No response item may contain photos, work orders, driver data, SAP metadata, or other UI-specific fields.

## Cursor Semantics

- Sort eligible units by numeric `id` ascending.
- The cursor represents the position after the final item returned by the previous page.
- A request with a cursor returns only eligible units after that position.
- Treat cursors as opaque client values. The implementation may encode the final `id`, but clients must not construct or modify cursors.
- If `hasMore` is `true`, `nextCursor` must be a non-empty string.
- If `hasMore` is `false`, `nextCursor` must be `null`.
- A page must not repeat IDs returned by an earlier page in the same traversal.

For an eligible fleet of 332 units and `limit=100`, the endpoint returns four pages containing 100, 100, 100, and 32 items.

## Suggested Query Strategy

Use keyset pagination rather than offset pagination. Fetch one extra row to determine whether another page exists.

```text
WHERE is_sap_found = true
  AND id > decodedCursorID
  AND no_lambung IS NOT NULL
  AND trim(no_lambung) <> ''
  AND no_polisi IS NOT NULL
  AND trim(no_polisi) <> ''
ORDER BY id ASC
LIMIT limit + 1
```

Return at most `limit` items. If an extra row exists, set `hasMore` to `true` and encode the final returned item ID as `nextCursor`.

## Error Responses

Use Lambung's standard error envelope.

```json
{
  "message": "limit must be between 1 and 200",
  "success": false,
  "code": 400
}
```

| HTTP status | When |
| --- | --- |
| `400` | `cursor` is invalid or `limit` is outside the allowed range. |
| `401` | The Authorization header is missing or the service token is invalid. |
| `403` | The authenticated service lacks access to the internal unit snapshot projection. |
| `500` | An unexpected server error occurred. Do not expose internal details. |

## Operational Requirements

- Return `Content-Type: application/json` for every success and error response.
- Return only the documented response shape for successful requests.
- Add `Cache-Control: no-store` because UnitLog must consume current source data.
- Do not log Authorization header values or service tokens.
- Add the endpoint and schemas to Lambung's OpenAPI or Swagger definition.

## Acceptance Criteria

- The endpoint accepts an initial request with no cursor and returns the first page.
- The endpoint accepts a returned cursor and returns the next page without duplicate IDs.
- The final page returns `hasMore: false` and `nextCursor: null`.
- Units where `isSapFound` is false never appear.
- Units with missing or blank `id`, `noLambung`, or `noPolisi` never appear.
- Response items include only `id`, `noLambung`, and `noPolisi`.
- Invalid cursor and limit values return `400` using the standard envelope.
- Missing, invalid, or unauthorized service tokens return `401` or `403`.
