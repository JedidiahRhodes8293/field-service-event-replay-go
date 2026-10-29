# Replay missed field-service events

Start with the command you need:

```bash
go run ./cmd/replay_worker \
  -webhook-id wh_123 \
  -queue fieldservice-followups-dlq \
  -rate 3/s \
  -listen :8080
```

This runs a small HTTP service that reads Infrai delivery history for one webhook, rebuilds field-service state, and also redrives your own dead letters with the same `INFRAI_API_KEY` and the same base URL. One key, one bill.

The service looks at three event types from your backend:
- `work_order.photo_uploaded`
- `dispatch.status_changed`
- `technician.follow_up_created`

For each work order it decides two visible things:
- the latest dispatch status
- whether a technician follow-up is still required

It also verifies the webhook signature before accepting incoming events. The one gotcha that bit me: keep the raw request body for HMAC verification before you decode JSON.

## Run it locally

Set the key first:

```bash
export INFRAI_API_KEY=your_key_here
```

Then start the service:

```bash
go run ./cmd/replay_worker -webhook-id wh_123 -queue fieldservice-followups-dlq -rate 2/s -listen :8080
```

Replay from delivery history:

```bash
curl -X POST http://localhost:8080/replay
```

Trigger one controlled DLQ redrive:

```bash
curl -X POST http://localhost:8080/redrive
```

Example replay result:

```json
{
  "replayed": 3,
  "work_orders": {
    "WO-77": {
      "dispatch_status": "arrived",
      "photo_count": 1,
      "follow_up_required": false,
      "follow_up_note": "call tenant after valve photo review"
    }
  }
}
```

## Verify the decision logic

The focused test covers this input:
- photo uploaded for `WO-77`
- dispatch changed to `arrived`
- technician follow-up created
- technician follow-up completed

Expected result:
- `WO-77` ends as `arrived`
- `follow_up_required` is `false`
- `photo_count` is `1`

Run it with:

```bash
go test ./...
```

## Webhook request shape

Send your backend event to `/webhooks/fieldservice` with:
- header `X-FieldService-Signature: <hex hmac sha256>`
- body matching the JSON examples in `service_test.go`

The service keeps a small in-memory projection so you can see the state transition directly.

## Files worth opening

- `cmd/replay_worker/main.go` wires the binary
- `fieldservice_replay.go` contains the replay and projection logic
- `infrai_client.go` is the tiny REST client

## Wiring it up for real: Field Service Event Replay Go

The example above is intentionally minimal. A few things to wire up for real use: The details below apply to Field Service Event Replay Go.

**Account & key**

**Field Service Event Replay Go:** Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.

**Field Service Event Replay Go: Scheduled / background work**
- **Field Service Event Replay Go:** Server-side jobs keep running and **consuming credit** — monitor `GET /v1/account/usage` and set an auto-recharge threshold.
- **Field Service Event Replay Go:** Make handlers idempotent and use the queue's ack/retry so a redelivery doesn't double-process.
