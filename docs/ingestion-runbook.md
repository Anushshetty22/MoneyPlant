# MoneyPlant Phase 1 Ingestion Runbook

This runbook describes the complete local Phase 1 data workflow. Each command
performs one understandable operation, and the database can be inspected after
every step.

## 1. Current provider status

| Dataset | Provider | Command | Current status |
|---|---|---|---|
| Crypto candles | Binance | `go run ./cmd/ingest-binance` | Real ingestion working |
| NSE EOD candles | Yahoo Finance | `go run ./cmd/ingest-yahoo` | Real ingestion working with retry/fallback |
| CPI | RBI DBIE CSV | `go run ./cmd/seed-macro` | Sample CSV working; official export pending |
| RBI repo rate | RBI DBIE CSV | `go run ./cmd/seed-macro` | Sample CSV working; official export pending |
| Indian equities/indexes | Angel One | Not currently available | Deferred until API setup is ready |

## 2. Prerequisites

From the repository root:

1. Start the PostgreSQL container.
2. Confirm the container exposes port `5432`.
3. Confirm the `psql` client is installed.
4. Confirm the database, user, and password match `.env.example`.

From the backend directory:

```bash
cd backend
go test ./...
```

The test suite uses local fakes and should not require API credentials.

## 3. Create the database schema on a fresh database

The migration files are numbered and must be applied in order. Run this from
the repository root against a fresh `moneyplant` database:

```bash
for migration in db/migrations/[0-9]*.sql; do
  PGPASSWORD=change-me-locally psql \
    -h localhost -p 5432 -U moneyplant -d moneyplant \
    -f "$migration" || exit 1
done
```

The migration files contain their own transaction boundaries. The final seed
migration creates the initial instruments, provider mappings, and macro dataset
definitions.

Do not run this loop against the already-migrated learning database unless you
intend to work through the expected “already exists” errors. The project does
not yet have a schema-migration tracking table; that is a future improvement.

## 4. Confirm initial definitions

```bash
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -c "SELECT canonical_symbol, asset_type, currency FROM instruments ORDER BY id;"

PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -c "SELECT i.canonical_symbol, s.provider, s.provider_symbol, s.is_authoritative
      FROM instrument_sources s
      JOIN instruments i ON i.id = s.instrument_id
      ORDER BY i.id, s.provider;"

PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -c "SELECT code, provider, metric, unit, frequency
      FROM macro_datasets ORDER BY id;"
```

## 5. Ingest Binance crypto candles

Run from `backend/`:

```bash
go run ./cmd/ingest-binance \
  --symbol BTCUSDT \
  --interval 1d \
  --from 2026-08-01T00:00:00Z \
  --to 2026-08-07T00:00:00Z
```

The end timestamp is exclusive. The command resolves `BTCUSDT` through the
database, calls Binance, stores normalized candles, and records a `market_api`
ingestion run.

## 6. Ingest Yahoo Finance NSE EOD data

Run from `backend/`:

```bash
go run ./cmd/ingest-yahoo \
  --symbol SBIN \
  --interval 1d \
  --from 2026-08-01T00:00:00Z \
  --to 2026-08-07T00:00:00Z
```

The command resolves the canonical `SBIN` instrument to `SBIN.NS`. Yahoo rows
are stored under provider `yahoo`, keeping them separate from future Angel One
rows for the same canonical instrument.

The adapter retries one rate-limited request and then tries the configured
fallback host. A persistent failure is recorded in `ingestion_runs`.

## 7. Seed macroeconomic CSV data

Run each dataset separately from `backend/`:

```bash
go run ./cmd/seed-macro \
  --dataset rbi_cpi_combined_yoy \
  --file ../data/seeds/rbi_cpi_combined_yoy.sample.csv

go run ./cmd/seed-macro \
  --dataset rbi_policy_repo_rate \
  --file ../data/seeds/rbi_policy_repo_rate.sample.csv
```

Macro observations are upserted by `(macro_dataset_id, observed_on)`. Running
the same file again updates existing rows instead of creating duplicates. The
sample files are learning fixtures; replace them with reviewed official RBI
exports before using them as final project data.

## 8. Inspect stored data

Market candles by provider:

```bash
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -c "SELECT i.canonical_symbol, s.provider, c.interval,
             COUNT(*) AS candle_count,
             MIN(c.observed_at) AS first_observation,
             MAX(c.observed_at) AS last_observation
      FROM market_candles c
      JOIN instrument_sources s ON s.id = c.instrument_source_id
      JOIN instruments i ON i.id = s.instrument_id
      GROUP BY i.canonical_symbol, s.provider, c.interval
      ORDER BY i.canonical_symbol, s.provider;"
```

Macro observations:

```bash
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -c "SELECT d.code, o.observed_on, o.value, o.source_row_reference
      FROM macro_observations o
      JOIN macro_datasets d ON d.id = o.macro_dataset_id
      ORDER BY d.code, o.observed_on;"
```

Recent ingestion audit records:

```bash
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -c "SELECT id, run_type, provider, status,
             rows_received, rows_inserted, rows_updated, rows_rejected,
             error_message
      FROM ingestion_runs
      ORDER BY id DESC
      LIMIT 20;"
```

## 9. Repeat-run behavior

| Operation | Repeat behavior |
|---|---|
| Macro CSV seed | Idempotent: existing dataset/date rows are updated |
| Binance candle ingestion | Current pipeline inserts; repeated identical candles can hit the unique constraint |
| Yahoo candle ingestion | Current pipeline inserts; repeated identical candles can hit the unique constraint |
| Failed provider request | A failed `ingestion_runs` row is retained for debugging |

Market-candle upsert behavior will be added in the data-quality and reliability
phase. Until then, use a new date window for repeated market-provider tests or
inspect the existing candle range before rerunning.

## 10. Troubleshooting guide

### PostgreSQL connection refused

Check `docker ps`, confirm `moneyplant-postgres` is running, and confirm port
`5432` is not occupied by another PostgreSQL instance.

### Unknown instrument or provider mapping

Inspect `instruments` and `instrument_sources`. The commands accept canonical
symbols, not arbitrary provider symbols.

### Yahoo HTTP 429

Do not run the command repeatedly in a short period. The adapter has bounded
retry and host failover. Inspect the failed `ingestion_runs` record, wait, and
retry a small date range.

### Macro CSV validation error

Confirm the header is exactly:

```text
observed_on,value,source_row_reference
```

Dates must use `YYYY-MM-DD`, values must be finite decimals, and one dataset
file cannot contain the same date twice.

### Partial ingestion run

Inspect `rows_inserted`, `rows_updated`, `rows_rejected`, and `error_message`.
The run is deliberately retained so the failure is visible instead of being
hidden by a command retry.
