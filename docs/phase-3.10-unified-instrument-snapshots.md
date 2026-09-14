# Phase 3.10 — Unified instrument and snapshot APIs

Phase 3.10 makes canonical symbols the only instrument identity the frontend
needs to know. Provider mappings are returned by the instrument endpoint, and
live snapshot filters resolve canonical symbols inside the Go API.

## API behavior

`GET /api/v1/instruments` now includes a `sources` array for every active
instrument. Each source identifies its provider, provider symbol, optional
provider instrument ID, and whether it is active and authoritative.

`GET /api/v1/live/snapshots` accepts optional `symbol` and `provider` query
parameters. `symbol` means the canonical MoneyPlant symbol, so callers can ask
for `TCS` without knowing that Angel One calls it `TCS-EQ`.

Examples:

```bash
curl http://localhost:8080/api/v1/instruments
curl "http://localhost:8080/api/v1/live/snapshots?symbol=TCS&provider=angel_one"
```

The frontend selects the active authoritative source returned by the backend
when requesting historical candles. Provider-specific symbols and tokens are
not hardcoded in React.

## Verification

From `backend/`:

```bash
go test ./internal/httpapi ./internal/ingestion
go test -run '^$' ./...
```
