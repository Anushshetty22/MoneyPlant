# MoneyPlant

MoneyPlant is a local-first financial data engineering and analytics project. It is being built as a learning project covering batch ingestion, relational data modeling, REST APIs, data visualization, and later AI-assisted analytics.

## Current milestone

Phase 1 development and Phase 8 integration are complete, and Phase 2.1
infrastructure work is now in progress. The project is intentionally
implemented incrementally, with each sub-phase producing a testable result and
a short learning checkpoint.

## Current progress

- Database schema and initial definitions are working locally.
- Binance real candle ingestion is working.
- Yahoo Finance NSE EOD fallback ingestion is working.
- CPI and RBI repo-rate CSV seeding is working with learning fixtures.
- Angel One integration is deferred until API setup is available.
- PostgreSQL now has a Docker Compose definition with persistent storage,
  health checks, and first-start migration initialization.

See [`docs/project-guide.md`](docs/project-guide.md) for the consolidated
setup, architecture, database, API, troubleshooting, and Phase 2 guide.

See [`docs/ingestion-runbook.md`](docs/ingestion-runbook.md) for the complete
Phase 1 workflow and verification commands.

## Repository layout

```text
backend/       Go ingestion engine and REST API
data/          Seed data and local data-flow documentation
db/            Versioned PostgreSQL migrations
docs/          Product, architecture, and learning notes
frontend/      Next.js read-only dashboard
infra/         Docker and local infrastructure configuration
```

## Planned Phase 1 data

- A small, configurable set of Indian equities and indices from Angel One
- A small set of cryptocurrency pairs from Binance
- Yahoo Finance as an NSE end-of-day fallback
- CPI inflation and RBI policy-rate macroeconomic series

The final source decisions will be recorded in `docs/data-source-decision.md` before the database schema is implemented.

## Development rule

Do not commit credentials or downloaded private data. Copy `.env.example` to `.env` for local configuration and keep `.env` untracked.

## Learning documents

- `docs/product-scope.md` - first working product and boundaries
- `docs/architecture.md` - services and data flow
- `docs/data-source-decision.md` - source-research template and decisions
- `docs/learning-notes.md` - concepts, commands, and problems encountered
