# Mobile Movement Cycle Handover

Use this document to show a shared `cycleId` for a truck departure and its return. A cycle is created and owned by UnitLog; the mobile app never sends a cycle ID.

## What A Cycle Means

Each movement still has its own `transactionId`. `cycleId` groups one departure with its return:

```text
OUT transaction #123, cycle #50
 IN transaction #124, cycle #50
```

- Every valid `OUT` creates a new cycle, including `BENGKEL_LUAR`, `FILLING_SHED_KUIN`, `CUSTOMER`, and `OTHER`.
- The next valid `IN` for that unit closes the open cycle and returns the same `cycleId`.
- A unit's first-ever `IN` is allowed but has `cycleId: null`, because there is no recorded departure to pair with it.
- A returning `IN` includes the linked departure transaction and its WO/SPP values when applicable.

## API Response Fields

`POST /api/v1/movements` and transaction-history responses include the additive cycle and departure fields below. Transaction-history responses additionally include the duration fields.

| Field | Type | Meaning |
| --- | --- | --- |
| `cycleId` | `number \| null` | Shared departure/return cycle identifier. |
| `departureTransactionId` | `number \| null` | The OUT transaction that started the cycle. |
| `departureWorkOrderNumber` | `string \| null` | WO from the departure, if the OUT used `BENGKEL_LUAR`. |
| `departureSppNumber` | `string \| null` | SPP from the departure, if the OUT used `FILLING_SHED_KUIN` or `CUSTOMER`. |
| `durationSeconds` | `number` | Transaction-history only: duration of the status in seconds; final after the next movement records. |
| `statusCompletedAt` | `string \| null` | Transaction-history only: server timestamp when the status ended; `null` while it remains current. |

### OUT example

```json
{
  "data": {
    "transactionId": 123,
    "cycleId": 50,
    "departureTransactionId": 123,
    "departureWorkOrderNumber": "WO-001",
    "departureSppNumber": null,
    "direction": "OUT",
    "outDestination": "BENGKEL_LUAR"
  }
}
```

### Returning IN example

```json
{
  "data": {
    "transactionId": 124,
    "cycleId": 50,
    "departureTransactionId": 123,
    "departureWorkOrderNumber": "WO-001",
    "departureSppNumber": null,
    "direction": "IN",
    "inCategory": "WAITING_FOR_ASSIGNMENT"
  }
}
```

### First-ever IN example

```json
{
  "data": {
    "transactionId": 12,
    "cycleId": null,
    "departureTransactionId": null,
    "departureWorkOrderNumber": null,
    "departureSppNumber": null,
    "direction": "IN"
  }
}
```

## Mobile Implementation Rules

1. **Do not add `cycleId` to a movement request.** UnitLog generates and resolves it atomically.
2. After a successful OUT, show the returned cycle ID in the confirmation and retain it only as display state.
3. After a successful returning IN, show the same cycle ID and the departure WO/SPP reference if present.
4. Render null departure fields as “No linked departure” rather than an empty or fabricated document number.
5. Refresh transaction history from UnitLog instead of attempting to correlate records locally by timestamp, unit code, WO, or SPP.
6. Reuse the same `Idempotency-Key` only for a retry of the exact same movement request. A successful retry returns the same transaction and cycle ID.
7. Show a completed duration from `durationSeconds` as final when `statusCompletedAt` is non-null. Current-status timers may be refreshed from UnitLog; do not overwrite completed durations with a device-calculated value.

## Valid Direction Flow

| Current unit status | Allowed next movement | Rejected next movement |
| --- | --- | --- |
| No recorded movement | IN or OUT | — |
| IN | OUT | IN |
| OUT | IN | OUT |

The backend is the authority. The mobile UI may disable an obviously invalid action using the unit's latest status, but it must still handle a `422 INVALID_TRANSITION` response because another scan may have changed the state first.

## Test Checklist

- Confirm an OUT returns a non-null `cycleId` equal to its `departureTransactionId` relationship.
- Confirm the following IN returns the same `cycleId` and departure WO/SPP reference.
- Confirm a first-ever IN returns null cycle/departure fields.
- Confirm `IN → IN` and `OUT → OUT` receive `422 INVALID_TRANSITION`.
- Confirm retrying a successful request with its original idempotency key returns the original cycle ID.
- Confirm the client does not include `cycleId` or departure fields in the POST request body.
- Confirm the previous OUT duration becomes final when a returning IN is recorded, and the previous IN duration becomes final when the next OUT is recorded.

## References

- [Full frontend handover](frontend-handover.md)
- [Mobile driver picker handover](mobile-driver-search-handover.md)
- Generated API definition: `http://172.16.17.17:8105/swagger-ui/index.html`
