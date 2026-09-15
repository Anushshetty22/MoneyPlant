# MoneyPlant Architecture

## Local services

### PostgreSQL

PostgreSQL is the persistent local warehouse. It stores normalized instruments, market candles, macroeconomic observations, ingestion-run metadata, and one latest live snapshot per monitored provider symbol.

### Go backend

The Go application has two responsibilities:

1. Batch ingestion from external APIs and local CSV files.
2. Read-only REST endpoints for the dashboard, including optional live
   monitoring status and latest snapshots.

Provider adapters should convert source-specific responses into common domain
records before persistence. The live path uses the same rule: Binance trade
messages and Angel One binary packets are normalized before monitoring, storage,
or HTTP delivery.

Phase 3.1 freezes the shared market vocabulary in
[`docs/phase-3-data-model.md`](phase-3-data-model.md). A canonical instrument
such as `SBIN` is resolved through `instrument_sources` to provider-specific
identities such as `SBIN.NS` or a future Angel One symbol/token. Provider
capabilities, live lifecycle states, and market errors are represented by
provider-neutral Go contracts before any provider-specific adapter is added.

### Next.js frontend

The Next.js application is a read-only presentation layer. It calls the Go API and renders charts, filters, loading states, empty states, and errors.

### Docker Compose

In Phase 2.1, Docker Compose runs PostgreSQL with a named volume, a health
check, and the ordered SQL migrations mounted into the official image's
first-start initialization directory. The backend and frontend continue to run
as local developer processes for now; they can be added as later Compose
services without changing the database boundary. Persistent database storage
must use a named volume so container restarts do not remove data.

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

## Phase 3 component boundaries

```text
Canonical instrument -> provider mapping -> provider adapter
        -> normalization/validation -> Repository -> PostgreSQL

Binance WebSocket --------------------+
                                     |
Angel One multiplexed WebSocket ------+-> LiveMarketMonitor
                                             |
                  +--------------------------+--------------------------+
                  |                          |                          |
           latest snapshot             1-minute candles             live status
        (memory + PostgreSQL)        (PostgreSQL upsert)          (status registry)
                  |                          |                          |
                  +--------------------------+--------------------------+
                                             |
                                      LiveStreamHub (SSE)
                                             |
                                      Next.js dashboard

PostgreSQL -> Go query handlers -> JSON REST API -> Next.js charts

PostgreSQL daily candles -> on-demand analytics engine -> analytics REST API
                                      -> dedicated Analytics dashboard view
```

The `instrument_sources` table is the authoritative bridge between a public
canonical symbol such as `TCS` and provider identities such as `TCS-EQ` or an
Angel One token. The dashboard selects that mapping from the API and does not
hardcode provider-specific symbols.

The live monitor remains useful when the market is closed: it can be running
with zero new events. Snapshot freshness and provider lifecycle status are
reported separately so a stale value is not automatically treated as a
connection failure.

Phase 4 analytics is deliberately computed on demand from normalized candles.
The engine uses exact rational arithmetic for ratios and fixed decimal strings
at the API boundary; it does not create a second derived-data warehouse.
