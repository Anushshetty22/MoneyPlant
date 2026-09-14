# Phase 3.11 — Server-Sent Events for dashboard updates

Phase 3.11 adds a provider-neutral Server-Sent Events stream so dashboard cards
can update as soon as the Go API receives a live event.

## Endpoint

```text
GET /api/v1/live/stream?symbol=TCS&provider=angel_one
```

The stream sends:

- `snapshot` events for latest market values;
- `status` events for monitor lifecycle changes; and
- `heartbeat` events every 15 seconds while the connection is idle.

The symbol filter uses the canonical MoneyPlant symbol. Multiple browsers can
subscribe independently, and closing one browser connection only removes that
subscriber. Provider monitors continue running. The dashboard still performs
an initial request and periodic polling so it can recover if the SSE connection
is temporarily unavailable.

## Verification

From `backend/`:

```bash
go test ./internal/httpapi ./cmd/api ./internal/ingestion
go test -run '^$' ./...
```

From `frontend/`:

```bash
npm run typecheck
```
