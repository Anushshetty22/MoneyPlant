# Phase 3.12 — Dashboard multi-source support

Phase 3.12 makes the dashboard provider-neutral. It uses the authoritative
source mapping returned by the Go API for every selected instrument, so the
same live card can display Binance crypto data or Angel One equity data.

## Dashboard behavior

- The instrument selector uses canonical symbols from the backend catalog.
- Historical candle requests use the selected instrument's authoritative
  provider automatically.
- Live snapshot and status requests use the canonical symbol and provider.
- The SSE stream updates price, quantity, trade time, receive time, and status
  for both providers.
- Running, reconnecting, disabled, error, and stale states are shown without
  treating an equity instrument as unsupported.
- Polling remains available as initial-load and stream-recovery fallback.

No provider token or provider-specific symbol is embedded in React. For
example, selecting `TCS` causes the backend to resolve Angel One's `TCS-EQ`
mapping.

## Verification

From `backend/`:

```bash
go test ./internal/httpapi ./cmd/api ./internal/ingestion
go test -race ./internal/httpapi ./cmd/api ./internal/ingestion
```

From `frontend/`:

```bash
npm run typecheck
```
