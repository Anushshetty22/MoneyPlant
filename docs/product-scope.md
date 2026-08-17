# MoneyPlant Product Scope

## Purpose

MoneyPlant is a local-first financial data platform for learning modern data engineering and analytics. It will collect historical market and macroeconomic data, store it in a relational warehouse, and present read-only analysis through a web dashboard.

## Intended users

The first user is the project builder: someone learning data engineering, analytics, machine learning, and AI-agent development through a practical financial-data system.

The system may later support personal finance users, but personal bank-statement data is not part of Phase 1.

## Phase 1 working product

The first working product will allow a user to:

- Start the local services with Docker.
- Load historical market data for a small configured instrument universe.
- Load CPI inflation and RBI policy-rate observations.
- Store normalized records in PostgreSQL.
- Re-run ingestion without creating duplicates.
- Query historical market and macroeconomic data through a read-only Go API.
- View price, volume, and macroeconomic time-series charts in a Next.js dashboard.

## Phase 1 data scope

### Market data

- Angel One: selected Indian equities and indices
- Binance: selected cryptocurrency OHLCV pairs
- Yahoo Finance: selected NSE end-of-day fallback symbols

The exact symbols, intervals, history ranges, and authoritative provider for each dataset will be finalized in `docs/data-source-decision.md`.

### Macroeconomic data

- Consumer Price Index inflation
- Reserve Bank of India policy rate

The initial macro files will include provenance metadata and their units.

## Explicitly deferred

- WebSocket streaming and live monitoring
- Text-to-SQL and local LLM agents
- Personal bank-statement ingestion
- Spending categorization
- Financial recommendations
- Public hosting and multi-user authentication
- Automated trading or investment advice

## Phase 1 data flow

```text
External APIs / CSV seeds
        |
        v
Go batch ingestion and validation
        |
        v
PostgreSQL warehouse
        |
        v
Go read-only REST API
        |
        v
Next.js dashboard
```

