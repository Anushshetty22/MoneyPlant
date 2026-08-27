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

For the NSE EOD fallback, use the canonical symbol `SBIN`. The command resolves
its provider-specific `SBIN.NS` mapping automatically:

```bash
go run ./cmd/ingest-yahoo \
  --symbol SBIN \
  --interval 1d \
  --from 2026-08-01T00:00:00Z \
  --to 2026-08-07T00:00:00Z
```

Yahoo ingestion stores unadjusted OHLCV values. Binance-specific fields such as
quote volume, trade count, and taker-buy volume remain NULL for these rows.

## Phase 4.5 macro CSV seeding

Run the sample CPI and repo-rate files independently:

```bash
go run ./cmd/seed-macro \
  --dataset rbi_cpi_combined_yoy \
  --file ../data/seeds/rbi_cpi_combined_yoy.sample.csv

go run ./cmd/seed-macro \
  --dataset rbi_policy_repo_rate \
  --file ../data/seeds/rbi_policy_repo_rate.sample.csv
```

Each command resolves the dataset definition, validates the CSV, upserts
observations by dataset and date, and records a `macro_seed` ingestion run.

For the complete Phase 1 execution order, verification queries, repeat-run
behavior, and troubleshooting guidance, see
[`docs/ingestion-runbook.md`](../docs/ingestion-runbook.md).
