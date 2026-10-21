# Mobile Active Driver Search Handover

Use this document to implement the Android/satpam driver picker for an `OUT` movement whose destination is `OTHER`.

The mobile app calls **UnitLog only**. It must never call the Driver service directly and must never receive, store, or log the Driver service token.

## Preconditions

- Obtain the UnitLog base URL from the mobile environment configuration. Development currently uses `http://172.16.17.17:8105`.
- Obtain the user access token through the approved Auth Service flow.
- The access token must include the `unitlog:app:default:view` permission.
- Actor ID and actor name claims are required when submitting a movement, but are not required for the driver-search request itself.

## Search Active Drivers

```http
GET /api/v1/drivers?name=<url-encoded-search-term>
Authorization: Bearer <satpam-access-token>
Accept: application/json
```

`name` is required. Trim the user input, then URL-encode it as a query parameter; do not construct the URL by string concatenation.

### Success response

```json
{
  "data": [
    {
      "id": 42,
      "driverName": "Driver Name"
    }
  ]
}
```

Only `id` and `driverName` are returned. Treat the returned list as a current search result, not a permanent local driver directory.

### Error response

All errors have this shape:

```json
{
  "error": {
    "code": "INVALID_DRIVER_QUERY",
    "message": "name query parameter is required"
  }
}
```

| Status | Code | Mobile behavior |
| --- | --- | --- |
| `400` | `INVALID_DRIVER_QUERY` | Do not send blank or whitespace-only searches. Clear results instead. |
| `401` | `UNAUTHORIZED` | Refresh or reacquire the Auth Service access token through the approved login flow. |
| `403` | `FORBIDDEN` | The user does not have the satpam permission; block the action and show an access issue. |
| `500` or `503` | `INTERNAL_ERROR` or `SERVICE_UNAVAILABLE` | Show a retryable service error. Do not expose upstream details to the user. |

An HTTP `200` with `data: []` is a valid result: no active driver matched the search term.

## Required Picker Flow

1. Show the driver picker only when `outDestination` is `OTHER`.
2. When the user changes away from `OTHER`, clear any selected driver.
3. Trim the search text. For normal UI behavior, wait until it contains at least two characters before searching; this is a mobile UX recommendation, not a server requirement.
4. Debounce requests (for example, 300 ms) and cancel or ignore stale in-flight searches when the text changes.
5. Render `driverName` and retain the matching numeric `id` internally.
6. Require the user to select a returned item. Do not allow a manually typed name to substitute for a selected driver.
7. Clear the selected item if the search text is edited after selection.
8. At submission, send the selected values as `driverId` and `driverName`.

By default, UnitLog validates the chosen driver again during movement submission. Operators may temporarily disable only that Driver call for OTHER IN/OUT using the server-side Consul flag `validate_other_driver_on_submit=false`. The active-driver picker and required positive `driverId`, nonblank `driverName`, and `note` remain unchanged. With the flag disabled, the stored driver snapshot is APP-reported, not verified active at submit. Do not send the flag from APP or relax picker selection; see [the OTHER validation handover](mobile-other-driver-validation-handover.md).

## OUT / OTHER Movement Submission

After selection, submit the normal movement request:

```http
POST /api/v1/movements
Authorization: Bearer <satpam-access-token>
Idempotency-Key: <new-uuid-for-this-confirmation>
Content-Type: application/json
```

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

For `OTHER`, `driverId`, `driverName`, and `note` are all required. Do not include `workOrderNumber` or `sppNumber`.

Generate one UUID when the user confirms this movement. Reuse it only when retrying the exact same movement body after an ambiguous network failure.

## Platform-Neutral Pseudocode

```text
onDestinationChanged(destination):
  if destination != "OTHER":
    clearDriverSelection()
    cancelDriverSearch()

onDriverSearchTextChanged(text):
  clearDriverSelection()
  query = trim(text)
  if length(query) < 2:
    showNoResultsYet()
    cancelDriverSearch()
    return

  debounce(300 ms):
    response = GET /api/v1/drivers?name=urlEncode(query)
      with Authorization: Bearer accessToken
    if response is 200:
      showDriverResults(response.data)
    else:
      showMappedError(response.error)

onDriverSelected(driver):
  selectedDriverID = driver.id
  selectedDriverName = driver.driverName

onSubmitMovement():
  require outDestination == "OTHER"
  require selectedDriverID and selectedDriverName and non-empty note
  POST /api/v1/movements with selected driver values
```

## Security Rules

- Keep the satpam access token in the platform's approved secure credential storage.
- Do not log `Authorization` headers, access tokens, full HTTP request headers, or any Driver service credentials.
- Do not embed the Driver service URL or service token in the app.
- Do not bypass UnitLog by calling the Driver service from the device.
- Render only the API's safe error code/message; do not show raw network or upstream error bodies.

## Mobile Test Checklist

- Search with a URL-reserved character or space in the name and verify the query is encoded.
- Confirm blank/whitespace input does not make a request.
- Confirm stale results cannot overwrite results for newer search text.
- Confirm a `200` empty list shows a no-results state.
- Confirm `401`, `403`, and `5xx` responses show the correct app state.
- Confirm changing away from `OTHER` clears the selected driver.
- Confirm the selected `id` and `driverName` are submitted unchanged in the `OUT / OTHER` movement request.
- Confirm no logs, analytics events, crash reports, or screenshots contain authorization tokens.

## References

- Full UnitLog mobile contract: [frontend-handover.md](frontend-handover.md)
- Generated UnitLog API definition: `http://172.16.17.17:8105/swagger-ui/index.html`
- Driver search endpoint: `GET /api/v1/drivers?name=...`
