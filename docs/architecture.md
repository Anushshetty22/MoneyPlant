# MoneyPlant Architecture

## Local services

### PostgreSQL

PostgreSQL is the persistent local warehouse. It stores normalized instruments, market candles, macroeconomic observations, and ingestion-run metadata.

### Go backend

The Go application has two responsibilities:

1. Batch ingestion from external APIs and local CSV files.
2. Read-only REST endpoints for the dashboard.

Provider adapters should convert source-specific responses into common domain records before persistence.

### Next.js frontend

The Next.js application is a read-only presentation layer. It calls the Go API and renders charts, filters, loading states, empty states, and errors.

### Docker Compose

Docker Compose will run PostgreSQL and, later, the backend and frontend. Persistent database storage must use a named volume so container restarts do not remove data.

## Design principles

- Local-first and zero-cost for development
- Small configurable datasets before broad ingestion
- Source-specific adapters with a common normalized model
- Idempotent ingestion
- Explicit provenance for every dataset
- Read-only presentation in Phase 1
- Fixture-based tests that work without network access or secrets
- Secrets only through environment configuration

## Planned domain concepts

- Instrument: a tradeable equity, index, or cryptocurrency pair
- Candle: OHLCV observation for an instrument and interval
- Macro observation: dated economic metric and value
- Ingestion run: one attempt to load data from a provider or seed file

## Planned component boundaries

```text
Provider adapters -> Normalization/validation -> Repository -> PostgreSQL

PostgreSQL -> Go query handlers -> JSON REST API -> Next.js charts
```

