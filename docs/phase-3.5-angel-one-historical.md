# Phase 3.5 — Angel One historical candles

Phase 3.5 connects the authenticated Angel One session to the existing
provider-neutral historical candle pipeline. The adapter does not create a
second market-candle model or write directly to PostgreSQL.

## What was added

- `AngelOneHistoricalMarketDataProvider` calls SmartAPI's authenticated
  `getCandleData` endpoint with the current provider token.
- Supported normalized intervals are `1m`, `5m`, `15m`, `30m`, `1h`, and `1d`.
  Angel One's `4h` and `1w` values are rejected because the provider does not
  return those intervals directly.
- Requests are split at Angel One's documented maximum range for each
  interval. A minute request larger than 30 days, for example, becomes several
  bounded provider requests.
- Provider timestamps such as `+05:30` are converted to UTC before storage.
- OHLCV values remain JSON raw values until they are parsed into PostgreSQL
  numerics, so they do not pass through `float64`.
- Responses are filtered to MoneyPlant's half-open `[From, To)` contract and
  duplicate timestamps are removed before the shared ingestion service writes
  them.
- The existing `MarketIngestionService` supplies `instrument_source_id`,
  validation, idempotent upserts, and ingestion-run audit records.

## Run a real historical import

First refresh the Angel One catalog so the database contains current provider
tokens, then run the authenticated command from `backend/`:

```bash
go run ./cmd/catalog-angelone --file testdata/angel_one_instrument_master.json

go run ./cmd/ingest-angelone \
  --symbol NIFTY50 \
  --interval 1d \
  --from 2026-08-01T00:00:00Z \
  --to 2026-08-07T00:00:00Z
```

The command reads the same local Angel One variables used by
`cmd/auth-angelone`. It logs only the run and row counts; JWT, refresh, feed,
API-key, PIN, and TOTP values remain out of logs and PostgreSQL.

The `--to` timestamp is exclusive. Repeating the same command is safe because
the shared candle repository upserts on the natural market-candle key.

## Verification

Run the focused adapter checks without requiring credentials or network access:

```bash
go test -run '^TestAngelOneHistoricalProvider' ./internal/ingestion -v
```

The tests verify authenticated request headers, IST request formatting, exact
decimal conversion, large-window splitting, malformed-row rejection, and
unsupported-interval rejection.
