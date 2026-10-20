# APP Agent Handover: Optional Movement Coordinates

## Scope

Extend the existing `POST /api/v1/movements` request with **one optional string**, `coordinates`, for both IN and OUT and every WO/SPP/OTHER source. Do not send separate `latitude` / `longitude` properties, an object, an array, or a JSON number.

This is client-reported location metadata for the immutable movement audit record. UnitLog does not verify GPS authenticity or enforce a geofence. Existing movement forms, source validation, actor claims, and permissions do not change.

## Request Examples

OUT with a work order:

```json
{
  "noLambung": "SAMT119",
  "direction": "OUT",
  "outDestination": "BENGKEL_LUAR",
  "workOrderNumber": "WO-001",
  "coordinates": "-3.319437,114.590889"
}
```

Returning IN with that same work order:

```json
{
  "noLambung": "SAMT119",
  "direction": "IN",
  "inCategory": "WAITING_FOR_ASSIGNMENT",
  "workOrderNumber": "WO-001",
  "coordinates": "-3.319438,114.590890"
}
```

The IN coordinate value is independent of its departure's location. The departure document still must match as documented in [the movement handover](mobile-document-movement-handover.md).

## Format And Optionality

| Rule | Contract |
| --- | --- |
| Order | `latitude,longitude` (not longitude first). |
| Numbers | Signed decimal values using `.` as the decimal point and exactly one comma between components. Exponent notation, NaN, infinity, and locale decimal commas are not accepted. |
| Ranges | Latitude `-90..90`; longitude `-180..180`, inclusive. |
| Whitespace | BE trims whitespace around both components; `" -3.319437 , 114.590889 "` is stored as `"-3.319437,114.590889"`. |
| Precision | Supplied decimal precision is preserved; BE does not round or truncate it. Normalized pair must be at most 100 characters. |
| Unavailable | Omit `coordinates` (preferred), or send `null`, `""`, or whitespace-only input. It is stored as null. |
| Zero | `"0,0"` is a valid supplied location; do not use it as a placeholder for unavailable GPS. |

## APP Changes

1. Add a nullable/optional string property `coordinates` to the movement request DTO and pending-submission storage. Response DTOs must also accept string or null.
2. Use the app's approved foreground location provider and permission flow. Do not add background/continuous tracking for this feature.
3. For a **new** movement, obtain available location before finalizing the confirmed request. Build the string with a locale-independent decimal point, in latitude/longitude order. Do not emit scientific notation.
4. If GPS permission is denied, GPS is unavailable, or the location attempt fails, allow the movement to proceed without this field; do not substitute a fake value or block the workflow solely for missing location.
5. Freeze/persist the complete payload and one idempotency UUID before sending. Disable duplicate submission while in flight.
6. Retrying must resend the original payload and key. Do not reacquire GPS, add the field to old pending requests, or modify location under the same key after a timeout or app upgrade.
7. Do not send location to document lookup, unit picker, or driver search endpoints. Do not log coordinates, request bodies containing them, or auth headers in client analytics/debug logs.

## Responses And Errors

- Successful movement creation/replay returns `data.coordinates` as a string or explicit null. Existing records have null; never infer a historical location from current GPS or the pool's address.
- Authorized transaction-history JSON (`GET /api/v1/transactions` and `GET /api/v1/units/{id}/transactions`) also returns nullable `coordinates`. Unit lists, pickers, and CSV output are unchanged.
- `400 INVALID_REQUEST`: wrong JSON type (for example an array or number).
- `422 VALIDATION_ERROR`: a nonblank string has invalid format, length, or range.
- `409 IDEMPOTENCY_CONFLICT`: the existing key is reused with different movement data, including changed/added coordinates.
- All existing document, transition, authentication, and service errors still apply. A valid coordinate does not make an invalid movement valid.

## Deployment

1. Apply migration `000005_add_movement_coordinates.up.sql` before deploying the coordinate-aware BE.
2. Deploy BE before releasing the APP update. An old BE may ignore an unknown request field; do not assume it stored coordinates.
3. Older apps and pending requests without coordinates remain supported; their normalized request hash is unchanged.
4. Migration rollback refuses once coordinate snapshots exist, to avoid deleting immutable audit metadata. Do not edit historical movements to force rollback.

## APP Acceptance Checklist

- [ ] IN and OUT can submit one coordinate string while retaining their existing valid source payload.
- [ ] Missing/denied GPS does not block movement submission.
- [ ] Locale settings never introduce an extra comma or exponent notation.
- [ ] Zero and negative values are handled correctly; invalid ranges are prevented or shown as validation errors.
- [ ] Timeout/restart retries preserve the original key and exact pending payload, including absent coordinates.
- [ ] Historical null response fields are handled without fabrication.
- [ ] No location or credential values appear in logs/analytics.

## References

- [Document/picker movement handover](mobile-document-movement-handover.md)
- [Movement cycle handover](mobile-movement-cycle-handover.md)
- [Frontend API guide](frontend-handover.md)
- Generated API definition: `/swagger-ui/doc.json`.
