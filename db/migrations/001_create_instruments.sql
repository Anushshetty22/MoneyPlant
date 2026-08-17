-- MoneyPlant Phase 1, migration 001
-- Purpose: create the canonical instrument table used by all provider mappings.

-- Run this migration as one transaction so a failure does not leave a partial table.
BEGIN;

-- Store one stable internal identity for each financial instrument.
CREATE TABLE instruments (
    -- Database-generated key; provider symbols are not used as internal IDs.
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- MoneyPlant's canonical symbol, such as BTCUSDT, SBIN, or NIFTY50.
    canonical_symbol TEXT NOT NULL,

    -- Human-readable instrument name shown in logs and later in the dashboard.
    name TEXT NOT NULL,

    -- Phase 1 supports these asset categories; the constraint prevents invalid values.
    asset_type TEXT NOT NULL,

    -- Exchange or venue associated with the canonical instrument, when applicable.
    exchange TEXT,

    -- Three-letter uppercase quote or reporting currency, such as USD or INR.
    currency CHAR(3) NOT NULL,

    -- Inactive instruments remain available for historical queries but are excluded from new ingestion.
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    -- Flexible storage for small non-critical metadata that does not need its own column yet.
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,

    -- Audit timestamps for the canonical instrument record.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Canonical symbols are normalized to uppercase before insertion and lookup.
    CONSTRAINT instruments_canonical_symbol_uppercase
        CHECK (canonical_symbol = UPPER(canonical_symbol)),

    -- Prevent duplicate canonical identities.
    CONSTRAINT instruments_canonical_symbol_unique
        UNIQUE (canonical_symbol),

    -- Restrict Phase 1 to the asset types currently supported by the project.
    CONSTRAINT instruments_asset_type_allowed
        CHECK (asset_type IN ('crypto', 'equity', 'index')),

    -- Require a valid three-letter uppercase ISO-style currency code.
    CONSTRAINT instruments_currency_format
        CHECK (currency ~ '^[A-Z]{3}$')
);

-- Speed up queries that list active instruments by asset category.
CREATE INDEX instruments_asset_type_active_idx
    ON instruments (asset_type, is_active);

-- Make all changes from this migration permanent only after every statement succeeds.
COMMIT;
