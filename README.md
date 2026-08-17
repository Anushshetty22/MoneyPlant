# MoneyPlant

MoneyPlant is a local-first financial data engineering and analytics project. It is being built as a learning project covering batch ingestion, relational data modeling, REST APIs, data visualization, and later AI-assisted analytics.

## Current milestone

We are starting with Phase 0 and Phase 1:

1. Define the first working product and understand the architecture.
2. Establish the development environment and repository structure.
3. Research and finalize the initial data sources.
4. Build the local PostgreSQL warehouse and batch pipeline.
5. Expose read-only data through Go and visualize it with Next.js.

The project is intentionally being implemented incrementally. Each sub-phase should produce a testable result and a short learning checkpoint.

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

