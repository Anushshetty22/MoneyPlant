# Backend

The Go ingestion engine and read-only REST API will be implemented here.

## Phase 3.4 Angel One authentication

Copy `.env.example` to `.env` and fill the four local credential values. Then
verify the login without printing any token values:

```bash
go run ./cmd/auth-angelone
```

The JWT, refresh token, and feed token stay in memory for the process. Invalid
credentials produce a safe provider error without echoing secrets.

## Phase 3.3 Angel One instrument catalog

Refresh the six canonical Indian-market mappings from a local Angel One master
file. This command requires no Angel One credentials; it parses the provider
file, resolves the current tokens, and upserts `instrument_sources`:

```bash
go run ./cmd/catalog-angelone \
  --file testdata/angel_one_instrument_master.json
```

Use the latest local `OpenAPIScripMaster.json` in place of the fixture when
refreshing real provider tokens. See
[`docs/phase-3.3-angel-one-catalog.md`](../docs/phase-3.3-angel-one-catalog.md)
for the resolver rules and failure cases.

## Phase 3.5 Angel One historical candles

After refreshing the provider catalog, import a bounded historical range through
the shared normalized candle pipeline:

```bash
go run ./cmd/ingest-angelone \
  --symbol NIFTY50 \
  --interval 1d \
  --from 2026-08-01T00:00:00Z \
  --to 2026-08-07T00:00:00Z
```

Supported intervals are `1m`, `5m`, `15m`, `30m`, `1h`, and `1d`. The command
resolves the current Angel One token from `instrument_sources`, splits large
provider windows, normalizes timestamps to UTC, preserves decimal values, and
uses the existing idempotent `market_candles` upsert. See
[`docs/phase-3.5-angel-one-historical.md`](../docs/phase-3.5-angel-one-historical.md).

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

## Phase 2.2 Binance live trade stream

The first live-stream adapter opens Binance's public Spot raw trade stream for
one symbol and converts each provider message into the normalized
`ingestion.LiveMarketEvent` contract. The adapter uses the documented
`<symbol>@trade` URL format and keeps provider decimal strings exact.

The adapter is tested against an in-process WebSocket server, so the test does
not require Binance access or credentials:

```bash
go test ./internal/ingestion -v
```

Reconnect policy, live-tick persistence, and API/dashboard monitoring are later
Phase 2 steps. This adapter currently focuses on connection, decoding, and
normalization.

## Phase 2.3 reconnect policy

`ReconnectingLiveMarketStream` wraps the Binance adapter and retries failed
connections with bounded exponential backoff. The policy supports context
cancellation and injectable sleeping, which keeps retry tests fast and
deterministic.

Run the focused tests with:

```bash
go test ./internal/ingestion -v
```

The reconnect wrapper does not persist live events or expose them through the
API yet. Those responsibilities remain separate future steps.

## Phase 2.4 live snapshot store

`LiveMarketSnapshotStore` keeps the latest valid event for each provider symbol
in a thread-safe in-memory map. It can be passed directly as the
`LiveMarketEventHandler` for `LiveMarketMonitor`.

The store currently supports latest-value reads and deterministic listing. It
does not replace the historical PostgreSQL candle model; durable live-tick
storage and API exposure remain later design steps.

For concurrency verification, run:

```bash
go test -race ./internal/ingestion
```

## Phase 2.5 runnable live monitor

The `monitor-binance` command connects the Binance adapter, reconnecting stream,
monitor, and snapshot store. It prints each accepted trade and stops when you
press Ctrl+C. Use `--events` for a bounded manual test:

```bash
go run ./cmd/monitor-binance \
  --symbol BTCUSDT \
  --events 3
```

The command uses public Binance market data and does not require API keys. It
does not persist live events to PostgreSQL yet.

## Phase 2.6 live snapshot API

The API can now expose the latest in-memory live event through a read-only
endpoint. Live monitoring is opt-in: leave `LIVE_MONITOR_SYMBOL` empty to run
the API without opening a WebSocket connection, or set it before startup:

```bash
LIVE_MONITOR_SYMBOL=BTCUSDT go run ./cmd/api
```

While the API is running, query all available symbol snapshots or filter one
symbol from another terminal:

```bash
curl http://localhost:8080/api/v1/live/snapshots
curl 'http://localhost:8080/api/v1/live/snapshots?symbol=BTCUSDT'
```

The endpoint returns an empty `data` array when monitoring is disabled or when
the requested symbol has not received an event yet. Prices and quantities stay
JSON strings so exact decimal precision is preserved. These snapshots are
memory-only and disappear when the API process stops; historical candles still
come from PostgreSQL.

The dashboard reads this endpoint through the Next.js backend proxy. Its live
card refreshes every five seconds for the selected instrument. This is browser
polling for now; the backend remains responsible for the real-time Binance
WebSocket connection. The card marks data as stale when the backend receive
timestamp is more than 15 seconds old, giving the learner a first simple
operational signal without pretending that the snapshot is durable.

## Phase 2.9 live monitor status

The backend also exposes operational state separately from market values:

```bash
curl http://localhost:8080/api/v1/live/status
```

The response reports whether monitoring is `disabled`, `starting`, `running`,
`reconnecting`, `stopped`, or in `error`, along with event counters, the last
event timestamps, and the last error when one exists.

The dashboard reads this status endpoint alongside the snapshot endpoint, so
the browser can show the backend's actual state and counters instead of
guessing from price freshness alone.

The `reconnects` counter records retry attempts after a stream failure. A
temporary reconnect does not immediately become a terminal monitor error.

## Phase 2.11 reconnect metrics

The reconnecting stream now accepts an optional retry callback. The API monitor
uses it to increment `reconnects` whenever a failed connection will be retried.
This keeps retry policy separate from monitoring while making transient network
instability visible in the status endpoint and dashboard.

## Phase 2.12 durable latest live snapshot

The API now upserts the latest live value per provider symbol into the
`live_market_snapshots` table. It does not archive every raw trade. When the
API starts, it restores the durable latest values into the in-memory store
before the optional WebSocket monitor begins.

For an existing PostgreSQL volume, apply the new migration once from the
repository root:

```bash
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -f db/migrations/008_create_live_market_snapshots.sql
```

The monitor status also reports successful saves, the last save time, and any
database persistence warning. This distinguishes a healthy Binance connection
from a healthy connection whose snapshots cannot be stored.

It also reports `restored`, the number of durable snapshots loaded when the API
started. This makes restart recovery visible in the status endpoint.

## Phase 3.2 multiple live symbols

The API accepts `LIVE_MONITOR_SYMBOLS=BTCUSDT,ETHUSDT` to start one independent
Binance monitor per symbol. `LIVE_MONITOR_SYMBOL=BTCUSDT` remains supported for
single-symbol compatibility. Snapshots are keyed by provider and provider
symbol, and each monitor has separate reconnect, event, and persistence
counters. The existing singular status endpoint shows the first configured
monitor until the plural status endpoint is added in Phase 3.8.

The live monitor accepts optional timing settings such as
`LIVE_MONITOR_PERSIST_INTERVAL=10s`, `LIVE_MONITOR_FINAL_PERSIST_TIMEOUT=3s`,
and `LIVE_MONITOR_RESTORE_TIMEOUT=5s`. The API validates these values, the
WebSocket URL, and the monitored symbol before startup.
