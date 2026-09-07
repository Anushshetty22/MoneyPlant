# MoneyPlant Phase 1 Completion Checklist

This checklist records the final state of the Phase 1 learning product. A
checked item means the capability was implemented and verified during the
project workflow. Deferred items are recorded explicitly so they are not
mistaken for unfinished or forgotten work.

## Infrastructure and repository

- [x] PostgreSQL runs locally through Docker.
- [x] PostgreSQL data uses a persistent Docker volume.
- [x] Repository structure is organized into backend, frontend, database,
  data, documentation, and infrastructure areas.
- [x] Git history is maintained with incremental commits.
- [x] The repository is pushed to GitHub.

## Database

- [x] Numbered migrations create the complete schema.
- [x] Instruments and provider-specific source mappings are modeled separately.
- [x] Market candles use normalized OHLCV storage and natural-key uniqueness.
- [x] Macro datasets and dated observations are modeled separately.
- [x] Ingestion runs record operational history and row counts.
- [x] Initial definitions can be recreated on a clean database.
- [x] `sqlc` generates type-safe database access code from SQL queries.

## Ingestion

- [x] Fixture-based market ingestion works without network access.
- [x] Binance daily cryptocurrency ingestion works with real API data.
- [x] Yahoo Finance NSE daily fallback ingestion works with real API data.
- [x] CPI CSV seeding works.
- [x] RBI policy repo-rate CSV seeding works.
- [x] Market records are validated before persistence.
- [x] Macro records are validated before persistence.
- [x] Market ingestion is idempotent.
- [x] Macro seeding is idempotent.
- [x] Failed provider requests remain visible in `ingestion_runs`.

## Go API

- [x] The API starts with environment-based configuration.
- [x] The API has health, instruments, market-candle, macro, and ingestion-run
  endpoints.
- [x] API query parameters are validated.
- [x] Financial numeric values preserve decimal precision in JSON responses.
- [x] API errors use predictable HTTP status codes and JSON responses.
- [x] Request logging and graceful shutdown are implemented.
- [x] Database and HTTP integration tests pass.

## Next.js dashboard

- [x] The frontend uses Next.js App Router and TypeScript.
- [x] Market instrument and date-range controls work.
- [x] Closing-price and volume charts work.
- [x] Macro dataset and date-range controls work.
- [x] Macro time-series charts work.
- [x] Loading, empty, and error states are displayed.
- [x] Source, unit, interval, frequency, and retrieval information is visible.
- [x] In-page navigation and responsive layout are implemented.
- [x] Frontend typechecking and production build pass.

## Documentation

- [x] Product scope is documented.
- [x] Architecture and data flow are documented.
- [x] Data-source decisions and limitations are documented.
- [x] Database design and rationale are documented.
- [x] The complete ingestion workflow is documented.
- [x] A consolidated project guide is available in `docs/project-guide.md`.
- [x] Learning notes and troubleshooting guidance are maintained.

## Intentionally deferred

- [ ] Angel One ingestion — deferred until API application setup, redirect URL,
  credentials, and required configuration are available.
- [ ] Automated official RBI DBIE export — current files are reviewed learning
  fixtures.
- [ ] WebSocket ingestion and live monitoring.
- [ ] Scheduler, production deployment, authentication, and alerting.
- [ ] Local LLM, Text-to-SQL, personal-finance, and ML features.

## Final verification commands

Run these checks from the indicated directories before declaring the current
repository state complete:

```bash
# From backend/
go test ./...
MONEYPLANT_RUN_INTEGRATION=1 go test ./internal/database -v
MONEYPLANT_RUN_INTEGRATION=1 go test ./internal/httpapi -v
```

```bash
# From frontend/
npm run typecheck
npm run build
```

The clean-database verification from Phase 8.2 should also remain successful.

## Completion statement

MoneyPlant Phase 1 is complete as a local-first learning MVP. The system can
load, validate, store, expose, and visualize market and macroeconomic data.
Angel One and the advanced analytics/AI capabilities are planned for later
phases rather than being required for the current working product.
