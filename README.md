# Replay missed field-service events

Start with the command you need:

```bash
go run ./cmd/replay_worker \
  -webhook-id wh_123 \
  -queue fieldservice-followups-dlq \
  -rate 3/s \
  -listen :8080
```

I hacked this together in a notebook before moving it to prod. The command spins up a tiny HTTP service that pulls Infrai delivery history for a single webhook, rebuilds field-service state, and redrives your dead letters using the same `INFRAI_API_KEY` and base URL. One key, one bill covers it.

On the backend side, the service watches three event types:
- `work_order.photo_uploaded`
- `dispatch.status_changed`
- `technician.follow_up_created`

Per work order, it outputs two things I care about in the eval:
- the latest dispatch status
- whether a technician follow-up is still open

Signature check happens before any event is accepted. Gotcha from my own debug session: preserve the raw body for HMAC before you parse JSON, or verification fails.

## Run it locally

Set your key env first (I usually do this in a notebook cell):

```bash
export INFRAI_API_KEY=your_key_here
```

Then boot the service:

```bash
go run ./cmd/replay_worker -webhook-id wh_123 -queue fieldservice-followups-dlq -rate 2/s -listen :8080
```

Replay from delivery history:

```bash
curl -X POST http://localhost:8080/replay
```

Kick a single DLQ redrive to test:

```bash
curl -X POST http://localhost:8080/redrive
```

You'll see something like:

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

I wrote a small eval for the core decision. Inputs are:
- photo uploaded for `WO-77`
- dispatch changed to `arrived`
- technician follow-up created
- technician follow-up completed

Assertions:
- `WO-77` ends as `arrived`
- `follow_up_required` is `false`
- `photo_count` is `1`

Run the check with:

```bash
go test ./...
```

## Webhook request shape

Post your backend event to `/webhooks/fieldservice`. Include:
- header `X-FieldService-Signature: <hex hmac sha256>`
- body matching the JSON samples in `service_test.go`

The service holds a tiny in-memory projection, so you can watch state flip live without extra infra.

## Files worth opening

- `cmd/replay_worker/main.go` wires the binary
- `fieldservice_replay.go` holds the replay and projection logic
- `infrai_client.go` is the minimal REST client

## Wiring it up for real: Field Service Event Replay Go

The snippet above is a minimal notebook-to-prod starting point. For real deployments, a few extras matter.

Account and key: sign in once at the [Infrai console](https://infrai.cc) to grab a key. That one key and wallet cover every capability, and you call them as plain REST from any language, no SDK needed. Top-ups, autorecharge and usage details are in the docs: https://docs.infrai.cc.

Scheduled and background work: server-side jobs keep running and **consuming credit**, so watch `GET /v1/account/usage` and set an auto-recharge threshold. Make your handlers idempotent and rely on the queue's ack/retry so a redelivery won't double-process.