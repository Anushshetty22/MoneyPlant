# MoneyPlant Project Guide

## 1. Project purpose

MoneyPlant is a local-first financial data platform built as a learning
project. It demonstrates how external financial data moves through an ingestion
pipeline, a PostgreSQL data layer, a Go REST API, and a Next.js dashboard.

Phase 1 is intentionally read-only. It is designed to teach data engineering
and analytics foundations before adding live streaming, machine learning, or
AI agents.

## 2. Phase 1 capabilities

The completed Phase 1 system can:

- Run PostgreSQL locally in Docker with persistent storage.
- Recreate its schema using ordered SQL migrations.
- Store canonical instruments and provider-specific symbols.
- Ingest real daily candles from Binance and Yahoo Finance.
- Seed CPI and RBI repo-rate observations from CSV files.
- Validate market and macro records before persistence.
- Re-run ingestion safely through idempotent upserts.
- Record successful, partial, and failed ingestion runs.
- Expose read-only REST endpoints through Go.
- Display market and macro time series in a Next.js dashboard.

Angel One is intentionally deferred until API credentials and the required
application configuration are available.

## 3. System architecture

```text
Binance API       Yahoo Finance       RBI CSV files
      |                  |                  |
      +------------------+------------------+
                         v
              Go ingestion and validation
                         |
                         v
                  PostgreSQL warehouse
                         |
                         v
                    Go REST API
                         |
                         v
                 Next.js web dashboard
```

### PostgreSQL

PostgreSQL is the persistent local warehouse. It stores the instrument
catalog, provider mappings, normalized market candles, macro datasets, macro
observations, and ingestion audit records.

### Go backend

The Go backend has two roles:

1. Provider commands fetch, validate, normalize, and persist data.
2. Read-only HTTP handlers query PostgreSQL and return stable JSON contracts.

The database access layer uses `sqlc`. SQL queries are written explicitly in
`backend/db/query/`, and `sqlc generate` produces type-safe Go methods in
`backend/internal/database/generated/`.

### Next.js frontend

The frontend calls the Go API and renders the instrument catalog, market
candles, macroeconomic observations, filters, charts, source labels, retrieval
times, loading states, empty states, and error states.

## 4. Repository map

| Path | Purpose |
|---|---|
| `backend/cmd/` | Executable commands for the API and ingestion jobs |
| `backend/internal/config/` | Environment configuration and validation |
| `backend/internal/database/` | Repositories, generated sqlc code, and database tests |
| `backend/internal/httpapi/` | REST routes, handlers, responses, and HTTP tests |
| `backend/internal/ingestion/` | Providers, validation, pipelines, and ingestion interfaces |
| `backend/db/query/` | SQL statements used by sqlc |
| `db/migrations/` | Ordered PostgreSQL schema and definition migrations |
| `data/fixtures/` | Offline market-provider response fixtures |
| `data/seeds/` | CSV files used to seed macroeconomic observations |
| `docs/` | Architecture, design, source decisions, runbooks, and learning notes |
| `frontend/src/` | Next.js page, dashboard components, API client, and styling |
| `infra/` | Local infrastructure notes |

## 5. Local setup

### Start PostgreSQL

From the repository root, start the Phase 2.1 PostgreSQL service with:

```bash
docker compose -f infra/compose.yaml up -d
docker compose -f infra/compose.yaml ps
```

The expected local container is named `moneyplant-postgres` and exposes port
`5432`. Confirm it is running with:

```bash
docker ps
```

The database uses the local-development values in `.env.example`:

```text
database: moneyplant
user: moneyplant
host: localhost
port: 5432
```

### Create the schema

When PostgreSQL is started with the Phase 2.1 Compose file, the numbered
migrations are mounted into the official image's initialization directory and
run automatically on a new `moneyplant-postgres-data` volume. Confirm the
definitions with:

```bash
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -c "SELECT canonical_symbol FROM instruments ORDER BY id;"
```

For a manual PostgreSQL installation or a deliberately fresh database, the
ordered migration loop remains available from the repository root:

```bash
for migration in db/migrations/[0-9]*.sql; do
  PGPASSWORD=change-me-locally psql \
    -h localhost -p 5432 -U moneyplant -d moneyplant \
    -f "$migration" || exit 1
done
```

The migrations create six tables and insert the initial instrument, provider,
and macro-dataset definitions. They are not a general-purpose migration
runner: the project does not yet maintain a schema-version table.

### Start the backend

From `backend/`:

```bash
go run ./cmd/api
```

The API listens on `http://localhost:8080` by default.

### Start the frontend

From `frontend/`:

```bash
npm install
npm run typecheck
npm run dev
```

Open `http://localhost:3000` after the Go API is running.

## 6. Data loading commands

Run these commands from `backend/` after the schema exists.

### Binance candles

```bash
go run ./cmd/ingest-binance \
  --symbol BTCUSDT \
  --interval 1d \
  --from 2026-08-01T00:00:00Z \
  --to 2026-08-07T00:00:00Z
```

The `--from` value is inclusive and `--to` is exclusive. The command resolves
the canonical symbol through PostgreSQL, requests Binance klines, validates
them, and upserts them into `market_candles`.

### Yahoo Finance NSE fallback

```bash
go run ./cmd/ingest-yahoo \
  --symbol SBIN \
  --interval 1d \
  --from 2026-08-01T00:00:00Z \
  --to 2026-08-07T00:00:00Z
```

The command resolves canonical symbol `SBIN` to provider symbol `SBIN.NS`.
Yahoo values are unadjusted daily OHLCV values. Binance-only fields remain
`NULL` for Yahoo rows.

### Macro CSV seeds

```bash
go run ./cmd/seed-macro \
  --dataset rbi_cpi_combined_yoy \
  --file ../data/seeds/rbi_cpi_combined_yoy.sample.csv

go run ./cmd/seed-macro \
  --dataset rbi_policy_repo_rate \
  --file ../data/seeds/rbi_policy_repo_rate.sample.csv
```

Each CSV must use the header:

```text
observed_on,value,source_row_reference
```

Dates use `YYYY-MM-DD`, values are exact decimal values, and the reference
column preserves the source row or explanation used for the observation.

## 7. REST API reference

All endpoints return a top-level `data` property for successful responses and
an `error` property for client or server errors.

### Health

```text
GET /health
```

Returns `{ "status": "ok" }`. This confirms that the HTTP process is alive.

### Instruments

```text
GET /api/v1/instruments
```

Returns active canonical instruments, including symbol, name, asset type,
exchange, currency, and active status.

### Market candles

```text
GET /api/v1/candles?symbol=BTCUSDT&provider=binance&interval=1d&from=2026-08-01T00:00:00Z&to=2026-08-07T00:00:00Z
```

Required parameters are `symbol`, `provider`, `interval`, `from`, and `to`.
The range is half-open: `observed_at >= from` and `observed_at < to`.
Financial numeric fields are returned as strings to preserve decimal
precision. Timestamps are returned as UTC ISO-8601 strings.

### Macro datasets

```text
GET /api/v1/macro/datasets
```

Returns active dataset definitions, including provider, metric, unit,
frequency, source URL, and retrieval time.

### Macro observations

```text
GET /api/v1/macro/observations?dataset=rbi_cpi_combined_yoy&from=2026-01-01&to=2026-03-01
```

The `dataset` parameter is required. `from` and `to` are optional, but must be
provided together when used. The date range is also half-open.

### Ingestion history

```text
GET /api/v1/ingestion-runs?provider=binance&limit=20
```

The `provider` filter is optional. `limit` defaults to 20 and accepts values
from 1 through 100. Results include status, requested range, row counts,
errors, and scope details.

## 8. Database design summary

| Table | Stores | Important rule |
|---|---|---|
| `instruments` | One canonical row per asset or trading pair | Canonical symbols are uppercase and unique |
| `instrument_sources` | Provider-specific symbols and tokens | One authoritative active source per instrument |
| `market_candles` | Normalized OHLCV observations | Unique by source, interval, and observation time |
| `macro_datasets` | Meaning and provenance of a macro series | Dataset code is unique |
| `macro_observations` | One dated value for a macro series | Unique by dataset and observation date |
| `ingestion_runs` | Operational audit record for each load attempt | Status and completion fields must agree |

The detailed design and reasoning are in
`docs/MoneyPlant_Phase1_Database_Design.docx` and `docs/database-design.md`.

## 9. Verification and troubleshooting

Run the Go test suite from `backend/`:

```bash
go test ./...
```

Run frontend checks from `frontend/`:

```bash
npm run typecheck
npm run build
```

For database-backed integration tests:

```bash
MONEYPLANT_RUN_INTEGRATION=1 go test ./internal/database -v
MONEYPLANT_RUN_INTEGRATION=1 go test ./internal/httpapi -v
```

Common issues:

- **PostgreSQL connection refused:** check `docker ps`, port `5432`, and the
  environment values.
- **Unknown symbol or provider:** use canonical symbols configured in
  `instruments` and inspect `instrument_sources`.
- **Yahoo HTTP 429:** wait before retrying; the adapter has bounded retry and
  fallback behavior, and the failed run remains in `ingestion_runs`.
- **Duplicate rows:** this should not occur for supported ingestion paths;
  natural-key upserts report updates instead.
- **Empty dashboard:** confirm the Go API is running and that the selected
  date range contains stored rows.

The more detailed command-by-command workflow is in
`docs/ingestion-runbook.md`.

## 10. Known limitations

- RBI data currently uses reviewed learning CSV fixtures; an automated official
  RBI export is not yet implemented.
- Angel One ingestion is deferred until API application setup is available.
- Phase 1 supports batch ingestion, not WebSocket streaming.
- The dashboard is read-only and has no authentication.
- There is no production deployment, scheduler, alerting, or schema-version
  tracking table yet.
- The project does not provide investment advice or automated trading.

## 11. Phase 2 status and preparation

Phase 2.1 is in progress. The repository now includes
`infra/compose.yaml`, which runs PostgreSQL with a named volume, a health
check, and automatic first-start execution of the ordered migrations. The
backend and frontend remain local developer processes in this first
infrastructure slice.

The next planned capabilities are:

- WebSocket ingestion and live monitoring.
- More robust scheduling and operational monitoring.
- Local LLM and Text-to-SQL exploration.
- Personal-finance CSV ingestion and categorization.
- Advanced analytics and machine-learning features.

These should be added only after the Phase 1 contracts, provenance rules,
idempotency behavior, and testing workflow remain stable.

## 12. Related documents

- `docs/product-scope.md` — product purpose and boundaries
- `docs/architecture.md` — component architecture and principles
- `docs/data-source-decision.md` — source decisions and limitations
- `docs/database-design.md` — detailed relational design
- `docs/ingestion-runbook.md` — complete local workflow
- `docs/learning-notes.md` — concepts, commands, and problems learned
- `docs/document-style-guide.md` — shared document formatting rules
