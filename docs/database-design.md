# MoneyPlant Phase 1 Database Design

**Status:** Proposed design for review before SQL migrations.

## 1. Purpose and scope

This document defines the PostgreSQL database design for MoneyPlant Phase 1. It is detailed enough for another data engineer to understand the warehouse model, implement migrations, build repository functions, and preserve the reasoning behind the design.

Phase 1 stores historical market candles, macroeconomic observations, instrument identities, provider-specific identifiers, and ingestion-run metadata. The database is local-first and runs in PostgreSQL 18 through Docker.

The design does not store live ticks, order books, orders, holdings, account balances, personal finance transactions, ML features, or LLM-generated analysis.

## 2. Design goals

- Keep source-specific API shapes out of the normalized tables.
- Store one stable identity for each financial instrument.
- Preserve provider and provenance information so records can be audited.
- Prevent duplicate candles and duplicate macro observations at the database level.
- Use idempotent upserts so an ingestion job can be safely repeated.
- Represent money and prices with exact numeric types rather than binary floating-point types.
- Store timestamps consistently and make date semantics explicit for macroeconomic data.
- Support Binance, Yahoo Finance, and the future Angel One adapter without changing the core candle model.
- Keep the first schema understandable and small enough for learning.

## 3. High-level model

The design has six tables. Four are the main domain tables and two are supporting reference tables.

1. `instruments` stores canonical asset identities.
2. `instrument_sources` stores provider-specific symbols, tokens, and source metadata.
3. `market_candles` stores normalized OHLCV observations.
4. `macro_datasets` stores the definition and provenance of CPI and repo-rate series.
5. `macro_observations` stores dated numeric observations for those series.
6. `ingestion_runs` stores the audit record for every collection attempt.

The two supporting tables are necessary because the same canonical instrument can have different identifiers at different providers, and a macro observation needs a stable dataset definition rather than repeating CPI or repo-rate metadata on every row.

## 4. Relationship model

An instrument can have many provider mappings. A provider mapping can have many candles. A macro dataset can have many observations. An ingestion run describes the scope and result of a collection attempt.

```text
instruments 1 ---- many instrument_sources 1 ---- many market_candles

macro_datasets 1 ---- many macro_observations

ingestion_runs records the attempt, scope, counts, status, and errors
for market or macro ingestion.
```

`market_candles` references `instrument_sources` instead of storing a free-text symbol. This preserves the exact provider identity used to collect the record. The canonical instrument remains available through the relationship.

## 5. PostgreSQL conventions

### 5.1 Schema and identifiers

Phase 1 uses PostgreSQL's `public` schema and lowercase snake_case identifiers. Primary keys use `bigint GENERATED ALWAYS AS IDENTITY`. The application must not use provider symbols as internal primary keys because symbols and provider tokens can change.

### 5.2 Time and date handling

Market timestamps use `timestamptz` and are stored in UTC. A candle's `observed_at` means the beginning of the provider candle interval. The original provider close time is retained separately when available.

Macro observations use `date` in `observed_on`. CPI is a monthly reference period, not a precise market timestamp. Repo-rate data may represent an announcement date or an effective date, so the dataset records that meaning in `observation_type`. An optional `published_at` timestamp can preserve publication timing when available.

The Go API may serialize both forms as ISO-8601 values, but the database does not invent a time of day for a macro observation.

### 5.3 Numeric handling

Prices, volumes, rates, and macro values use PostgreSQL `numeric`, not `real` or `double precision`. Exact numeric storage avoids binary floating-point rounding during comparisons, validation, and display.

The initial proposed types are `numeric(30,10)` for prices and `numeric(38,18)` for volumes. Macro values use `numeric(20,8)`. These are capacity choices, not claims that every source provides that many meaningful decimal places.

### 5.4 Provenance

Every market candle identifies its provider mapping. Every macro dataset records its source URL, metric, unit, frequency, and retrieval metadata. Ingestion runs record when and how collection happened. Raw provider payloads are kept as local fixtures or source artifacts for tests; the normalized tables are not intended to be unbounded raw-payload storage.

## 6. Table: `instruments`

### Purpose

Stores the canonical identity of a financial instrument independent of any one provider. It prevents repeated names, asset classifications, exchanges, and currencies from being copied into every candle row.

### Columns

| Column | PostgreSQL type | Null? | Key/default | What it stores and why |
|---|---|---|---|---|
| `id` | `bigint` | No | Primary key, identity | Stable internal identifier referenced by provider mappings. |
| `canonical_symbol` | `text` | No | Unique | MoneyPlant's stable symbol, such as `BTCUSDT` or `SBIN`. |
| `name` | `text` | No |  | Human-readable name, such as `Bitcoin / Tether` or `State Bank of India`. |
| `asset_type` | `text` | No | Check constraint | `crypto`, `equity`, or `index` in Phase 1. |
| `exchange` | `text` | Yes |  | Market venue such as Binance, NSE, or an index publisher. |
| `currency` | `char(3)` | No |  | Quote or reporting currency, such as `USD` or `INR`. |
| `is_active` | `boolean` | No | Default `true` | Whether new ingestion jobs should include the instrument. Historical rows remain queryable when it becomes inactive. |
| `metadata` | `jsonb` | No | Default `{}` | Small, non-critical instrument attributes that do not justify a new column yet. |
| `created_at` | `timestamptz` | No | Default `now()` | When the internal record was created. |
| `updated_at` | `timestamptz` | No | Default `now()` | When canonical metadata was last changed. |

### Constraints and indexes

- Primary key: `id`.
- Unique constraint: `canonical_symbol`.
- Check: `canonical_symbol = upper(canonical_symbol)`.
- Check: `asset_type` must be one of the Phase 1 values.
- Check: `currency` must be three uppercase letters.
- Index: `(asset_type, is_active)` for instrument selection.

## 7. Table: `instrument_sources`

### Purpose

Stores the provider-specific identity for an instrument. This table is important because one asset can be represented as `SBIN` at Angel One, `SBIN.NS` at Yahoo Finance, and by a symbol/token pair at another provider. It also allows the authoritative provider and fallback provider to coexist without confusing their records.

### Columns

| Column | PostgreSQL type | Null? | Key/default | What it stores and why |
|---|---|---|---|---|
| `id` | `bigint` | No | Primary key, identity | Stable internal identifier for one provider mapping. |
| `instrument_id` | `bigint` | No | Foreign key | Links the provider mapping to the canonical instrument. |
| `provider` | `text` | No |  | `binance`, `yahoo`, or `angel_one`. |
| `provider_symbol` | `text` | No |  | Exact symbol sent to the provider, such as `SBIN.NS` or `BTCUSDT`. |
| `provider_instrument_id` | `text` | Yes |  | Provider token or instrument ID, especially needed for Angel One. |
| `is_authoritative` | `boolean` | No | Default `false` | Marks the preferred source for this instrument in the configured ingestion policy. |
| `is_active` | `boolean` | No | Default `true` | Controls whether this mapping may be used for new collection. |
| `metadata` | `jsonb` | No | Default `{}` | Provider-specific exchange, market segment, or symbol-resolution details. |
| `created_at` | `timestamptz` | No | Default `now()` | Mapping creation time. |
| `updated_at` | `timestamptz` | No | Default `now()` | Last mapping update time. |

### Constraints and indexes

- Primary key: `id`.
- Foreign key: `instrument_id` references `instruments(id)` with deletion restricted.
- Unique constraint: `(provider, provider_symbol)`.
- Partial unique index: `(provider, provider_instrument_id)` where `provider_instrument_id IS NOT NULL`. This prevents duplicate provider tokens while allowing providers such as Yahoo Finance to omit numeric instrument IDs.
- Index: `(instrument_id, is_active)`.
- Partial unique index: `(instrument_id)` where `is_authoritative = TRUE`. Phase 1 permits at most one authoritative provider mapping per canonical instrument.

The partial indexes should be implemented explicitly in PostgreSQL:

```sql
CREATE UNIQUE INDEX uq_instrument_sources_provider_token
ON instrument_sources (provider, provider_instrument_id)
WHERE provider_instrument_id IS NOT NULL;

CREATE UNIQUE INDEX uq_one_authoritative_source
ON instrument_sources (instrument_id)
WHERE is_authoritative = TRUE;
```

## 8. Table: `market_candles`

### Purpose

Stores normalized historical OHLCV candles from market-data providers. Each row represents one interval for one provider mapping at one candle-open timestamp.

### Columns

| Column | PostgreSQL type | Null? | Key/default | What it stores and why |
|---|---|---|---|---|
| `id` | `bigint` | No | Primary key, identity | Internal row identity. |
| `instrument_source_id` | `bigint` | No | Foreign key | Exact provider mapping used to obtain the candle. |
| `interval` | `text` | No | Check constraint | Candle interval; Phase 1 initially uses `1d`. |
| `observed_at` | `timestamptz` | No | Unique-key component | UTC start time of the provider candle. |
| `source_close_at` | `timestamptz` | Yes |  | Provider close boundary, when supplied by Binance or another source. |
| `open` | `numeric(30,10)` | No |  | Opening price. |
| `high` | `numeric(30,10)` | No |  | Highest price in the interval. |
| `low` | `numeric(30,10)` | No |  | Lowest price in the interval. |
| `close` | `numeric(30,10)` | No |  | Closing price used for trends and charts. |
| `volume` | `numeric(38,18)` | No |  | Base-asset or provider-defined traded volume. |
| `quote_volume` | `numeric(38,18)` | Yes |  | Quote-asset turnover, available from Binance. |
| `trade_count` | `bigint` | Yes |  | Number of trades, available from Binance. |
| `taker_buy_volume` | `numeric(38,18)` | Yes |  | Binance taker-buy base volume. |
| `taker_buy_quote_volume` | `numeric(38,18)` | Yes |  | Binance taker-buy quote volume. |
| `source_retrieved_at` | `timestamptz` | No |  | When the source response was retrieved. |
| `created_at` | `timestamptz` | No | Default `now()` | First insertion time. |
| `updated_at` | `timestamptz` | No | Default `now()` | Last upsert update time. |

The provider-specific Binance columns are intentionally retained as explicit nullable columns. Yahoo Finance, Angel One, and other equity sources may not provide these values, so `NULL` means “not supplied by this provider” and must not be converted to zero. Explicit columns keep the fields typed, easy to validate, easy to query, and straightforward for a learner to understand. A future `source_metadata jsonb` extension may hold genuinely uncommon provider fields, but it does not replace these important Phase 1 columns.

### Constraints and indexes

- Primary key: `id`.
- Foreign key: `instrument_source_id` references `instrument_sources(id)` with deletion restricted.
- Unique constraint: `(instrument_source_id, interval, observed_at)`.
- Check: `open`, `high`, `low`, `close`, and `volume` are non-negative.
- Check: `high >= greatest(open, close)` and `low <= least(open, close)`.
- Check: `high >= low`.
- Check: `trade_count` is non-negative when present.
- Check: `source_close_at` is after or equal to `observed_at` when present.
- Index: `(instrument_source_id, interval, observed_at DESC)` for chart-range queries.

### Duplicate and upsert behavior

The unique constraint is the database guarantee against duplicate candles. The ingestion repository will use an upsert keyed by `(instrument_source_id, interval, observed_at)`. A repeated source response updates the existing numeric values and retrieval metadata rather than creating a second row. The ingestion run records how many rows were inserted, updated, rejected, or skipped.

## 9. Table: `macro_datasets`

### Purpose

Stores the definition of a macroeconomic series. It prevents every CPI or repo-rate observation from repeating the source name, unit, frequency, and dataset meaning.

### Columns

| Column | PostgreSQL type | Null? | Key/default | What it stores and why |
|---|---|---|---|---|
| `id` | `bigint` | No | Primary key, identity | Internal dataset identity. |
| `code` | `text` | No | Unique | Stable code such as `rbi_cpi_combined_yoy` or `rbi_policy_repo_rate`. |
| `name` | `text` | No |  | Human-readable series name. |
| `provider` | `text` | No |  | Source provider, initially `rbi_dbie`. |
| `metric` | `text` | No |  | Exact measure, such as `CPI (combined)` or `Policy repo`. |
| `unit` | `text` | No |  | `percent`, index points, or another explicit unit. |
| `frequency` | `text` | No | Check constraint | `monthly`, `daily`, or `event`. |
| `observation_type` | `text` | No |  | Meaning of the date, such as `reference_period`, `announcement_date`, or `effective_date`. |
| `base_period` | `text` | Yes |  | CPI base year or other index-definition period, if applicable. |
| `source_url` | `text` | No |  | Official portal or publication location. |
| `retrieved_at` | `timestamptz` | No |  | When the dataset metadata or seed was reviewed. |
| `is_active` | `boolean` | No | Default `true` | Whether the dataset is used in new seed operations. |
| `metadata` | `jsonb` | No | Default `{}` | Source notes, export limitations, and additional definitions. |
| `created_at` | `timestamptz` | No | Default `now()` | Dataset record creation time. |
| `updated_at` | `timestamptz` | No | Default `now()` | Last metadata update time. |

### Constraints and indexes

- Primary key: `id`.
- Unique constraint: `code`.
- Check: `unit` and `frequency` are not blank.
- Index: `(provider, is_active)`.

## 10. Table: `macro_observations`

### Purpose

Stores one numeric observation for one defined macro dataset and one reference date. It is separate from market candles because CPI and repo rates do not have OHLCV structure.

### Columns

| Column | PostgreSQL type | Null? | Key/default | What it stores and why |
|---|---|---|---|---|
| `id` | `bigint` | No | Primary key, identity | Internal row identity. |
| `macro_dataset_id` | `bigint` | No | Foreign key | Identifies the exact metric, unit, frequency, and source definition. |
| `observed_on` | `date` | No | Unique-key component | Monthly reference period or policy event date. |
| `value` | `numeric(20,8)` | No |  | Numeric macroeconomic observation. |
| `source_retrieved_at` | `timestamptz` | No |  | When this value was retrieved or reviewed. |
| `source_row_reference` | `text` | Yes |  | Optional source row, publication, or export reference. |
| `metadata` | `jsonb` | No | Default `{}` | Notes about missing values, revisions, or date interpretation. |
| `created_at` | `timestamptz` | No | Default `now()` | First insertion time. |
| `updated_at` | `timestamptz` | No | Default `now()` | Last upsert update time. |

### Constraints and indexes

- Primary key: `id`.
- Foreign key: `macro_dataset_id` references `macro_datasets(id)` with deletion restricted.
- Unique constraint: `(macro_dataset_id, observed_on)`.
- Index: `(macro_dataset_id, observed_on DESC)` for time-series queries.
- The database does not interpolate repo-rate observations into daily rows.

### Duplicate and upsert behavior

The unique dataset/date key prevents duplicate observations. Re-seeding the same CPI or repo-rate file updates the value and provenance fields for that dataset/date. A changed source value is therefore visible as an update during the ingestion run rather than producing a second conflicting row.

## 11. Table: `ingestion_runs`

### Purpose

Stores the operational audit record for each batch collection or seed attempt. It is the pipeline's flight recorder for rate limits, network failures, parser failures, empty responses, and successful loads.

### Columns

| Column | PostgreSQL type | Null? | Key/default | What it stores and why |
|---|---|---|---|---|
| `id` | `bigint` | No | Primary key, identity | Run identity used in logs and troubleshooting. |
| `run_type` | `text` | No | Check constraint | `market_api`, `macro_seed`, or `migration`. |
| `provider` | `text` | No |  | Provider or source family used by the run. |
| `status` | `text` | No | Check constraint | `running`, `succeeded`, `partial`, or `failed`. |
| `started_at` | `timestamptz` | No | Default `now()` | Run start time. |
| `completed_at` | `timestamptz` | Yes |  | End time; null while running. |
| `requested_from` | `timestamptz` | Yes |  | Requested market start boundary. |
| `requested_to` | `timestamptz` | Yes |  | Requested market end boundary. |
| `rows_received` | `bigint` | No | Default `0` | Rows returned by the source or read from the file. |
| `rows_inserted` | `bigint` | No | Default `0` | New normalized rows created. |
| `rows_updated` | `bigint` | No | Default `0` | Existing rows changed by upsert. |
| `rows_rejected` | `bigint` | No | Default `0` | Rows failing validation. |
| `error_message` | `text` | Yes |  | Human-readable failure summary. |
| `scope` | `jsonb` | No | Default `{}` | Symbols, datasets, intervals, and command options included in the run. |
| `created_at` | `timestamptz` | No | Default `now()` | Audit-row creation time. |

### Constraints and indexes

- Primary key: `id`.
- Check: all row counters are non-negative.
- Check: `completed_at` is null only for `running` status, unless a future recovery policy allows otherwise.
- Index: `(provider, started_at DESC)`.
- Index: `(status, started_at DESC)`.

An ingestion run may cover multiple symbols or datasets, so its scope is stored in `jsonb` rather than forcing one foreign key to one target row.

## 12. Data-quality rules

The database enforces structural rules; the Go normalization layer enforces provider-specific rules before insertion.

- Candle timestamps must be UTC after conversion.
- Candle prices and volumes cannot be negative.
- `high` must be at least the greater of open and close.
- `low` must be at most the lesser of open and close.
- A candle must have all common OHLCV values after normalization.
- Macro values must be numeric and their unit must come from the dataset definition.
- Missing source values are not silently converted to zero.
- A source timestamp must be retained or documented when a provider's close boundary is available.
- Provider identity must never be inferred from a human-readable name alone.

## 13. Query patterns the design must support

The schema is designed for these Phase 1 queries:

- List active instruments and their provider mappings.
- Return candles for one canonical instrument and interval between two UTC timestamps. The repository resolves the canonical symbol through `instruments` and `instrument_sources`, then uses the indexed `instrument_source_id` path. A join is acceptable for the initial implementation; a two-step lookup can be evaluated later with query plans if scale requires it.
- Return the latest N candles ordered by `observed_at`.
- Return CPI observations between two reference dates.
- Return repo-rate observations with their date meaning.
- Show the latest successful or failed ingestion runs for a provider.
- Compare authoritative and fallback market-source records without merging their identities.

## 14. Transaction and upsert rules

Each ingestion batch uses a transaction boundary appropriate to the batch size. Instrument/provider mapping creation and its candle upserts should be committed together when the mapping is created during ingestion. A failed transaction must not leave half of a batch marked as successful.

The repository records an ingestion run as `running`, performs validated inserts/upserts, updates row counts, and closes the run as `succeeded`, `partial`, or `failed`. A network or parser error must be stored in `error_message` and returned to the command layer.

Upserts must target the explicit unique keys. They must not use a broad delete-and-reload strategy because historical data and auditability should survive a partial provider failure.

## 15. Deletion and retention policy

Phase 1 does not automatically delete market or macro observations. Instruments, provider mappings, and dataset definitions are deactivated with `is_active=false` rather than deleted. Foreign keys use restricted deletion so a metadata cleanup cannot accidentally remove historical observations.

Raw fixtures may be replaced when a better sanitized sample is captured, but normalized database rows are retained unless a deliberate migration or correction process is documented.

## 16. Migration plan

The proposed migration sequence is:

1. `001_create_instruments.sql`: create `instruments` and base constraints.
2. `002_create_instrument_sources.sql`: create provider mappings and foreign keys.
3. `003_create_market_candles.sql`: create OHLCV storage, checks, unique key, and indexes.
4. `004_create_macro_datasets.sql`: create macro-series definitions.
5. `005_create_macro_observations.sql`: create observations, unique key, and indexes.
6. `006_create_ingestion_runs.sql`: create operational audit storage.
7. `007_seed_initial_definitions.sql`: seed the selected instruments and macro dataset definitions without secrets.

Each migration must be executable in order on a clean database and must be tested against the Docker PostgreSQL instance. Seed data belongs in a separate migration or seed command so schema creation remains independent from changing source selections.

## 17. Example normalized records

### Market candle

```text
instrument: BTCUSDT
provider: binance
interval: 1d
observed_at: 2026-08-06T00:00:00Z
open: 64665.24000000
high: 64999.00000000
low: 64172.00000000
close: 64323.61000000
volume: 9864.43284000
```

### Macro observation

```text
dataset: rbi_cpi_combined_yoy
observed_on: 2026-01-01
value: 2.73
unit: percent
observation_type: reference_period
```

### Ingestion run

```text
run_type: market_api
provider: binance
status: succeeded
rows_received: 5
rows_inserted: 5
rows_updated: 0
rows_rejected: 0
```

## 18. Locked decisions before migrations

- Canonical symbols are normalized to uppercase before insertion and lookup. The database enforces this with `CHECK (canonical_symbol = upper(canonical_symbol))`. Provider symbols retain the provider's exact format, such as `SBIN.NS`.
- The final Phase 1 instrument list and provider mappings.
- `quote_volume`, `trade_count`, `taker_buy_volume`, and `taker_buy_quote_volume` remain explicit nullable columns in the first migration. They are Binance-specific, but explicit typed columns are more useful for Phase 1 validation and learning. A future `source_metadata jsonb` column may be added for less common fields.
- `instrument_sources` uses a partial unique index on `(provider, provider_instrument_id)` where the token is not null.
- `instrument_sources` uses a partial unique index on `(instrument_id)` where `is_authoritative = TRUE`, ensuring one authoritative mapping per canonical instrument.
- The exact official downloadable seed source for CPI and repo rate; the RBI dashboard has been verified but did not provide a CSV export in the reviewed view.
- Macro dates remain SQL `date` values and are serialized by the Go API as ISO-8601 strings in `YYYY-MM-DD` format. The API does not convert them to UTC midnight.

These decisions do not change the core relationship model. They should be recorded in the migration README before implementation.

## 19. Design checkpoint

The database design is ready for review and migration implementation when the team can explain:

- Why provider mappings are separate from canonical instruments.
- Why market candles and macro observations are separate.
- Which unique keys make ingestion idempotent.
- How timestamps and macro reference dates differ.
- Which constraints protect OHLCV data quality.
- How an ingestion run explains a failed or partial load.
