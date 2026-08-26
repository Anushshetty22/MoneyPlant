# Backend

The Go ingestion engine and read-only REST API will be implemented here.

## Phase 4.2 Binance ingestion command

After PostgreSQL is running and migrations plus seed definitions have been applied,
run a bounded historical Binance batch from this directory:

```bash
go run ./cmd/ingest-binance \
  --symbol BTCUSDT \
  --interval 1d \
  --from 2026-08-01T00:00:00Z \
  --to 2026-08-07T00:00:00Z
```

The `--to` timestamp is exclusive. The command resolves `BTCUSDT` through the
canonical `instruments` table, finds its active Binance mapping in
`instrument_sources`, downloads klines, and records the batch in
`market_candles` and `ingestion_runs`.
