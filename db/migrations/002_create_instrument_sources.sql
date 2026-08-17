-- MoneyPlant Phase 1, migration 002
-- Purpose: map each canonical instrument to one or more data-provider identifiers.

-- Run this migration atomically so the table and all indexes are created together.
BEGIN;

-- Store provider-specific symbols, tokens, and source preferences separately from canonical instruments.
CREATE TABLE instrument_sources (
    -- Database-generated key for one provider mapping.
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- Link this provider mapping to the canonical instrument created in migration 001.
    instrument_id BIGINT NOT NULL,

    -- Normalized provider name, for example binance, yahoo, or angel_one.
    provider TEXT NOT NULL,

    -- Exact symbol sent to the provider, such as BTCUSDT or SBIN.NS.
    provider_symbol TEXT NOT NULL,

    -- Optional provider token; Angel One commonly requires this identifier.
    provider_instrument_id TEXT,

    -- Marks the preferred source used by the ingestion policy for this instrument.
    is_authoritative BOOLEAN NOT NULL DEFAULT FALSE,

    -- Inactive mappings remain available for historical records but are excluded from new ingestion.
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    -- Flexible storage for provider-specific resolution details and market metadata.
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,

    -- Audit timestamps for the provider mapping.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Prevent orphaned mappings and preserve historical mappings if an instrument is retired.
    CONSTRAINT instrument_sources_instrument_fk
        FOREIGN KEY (instrument_id)
        REFERENCES instruments (id)
        ON DELETE RESTRICT,

    -- Provider names are stored in lowercase for consistent filtering and uniqueness.
    CONSTRAINT instrument_sources_provider_format
        CHECK (provider <> '' AND provider = LOWER(provider)),

    -- A provider symbol is required and cannot be an empty string.
    CONSTRAINT instrument_sources_provider_symbol_present
        CHECK (provider_symbol <> ''),

    -- The same provider symbol must identify only one canonical instrument.
    CONSTRAINT instrument_sources_provider_symbol_unique
        UNIQUE (provider, provider_symbol)
);

-- Provider tokens are optional, so enforce uniqueness only for mappings that have a token.
CREATE UNIQUE INDEX instrument_sources_provider_token_unique_idx
    ON instrument_sources (provider, provider_instrument_id)
    WHERE provider_instrument_id IS NOT NULL;

-- Speed up lookups for active provider mappings belonging to one canonical instrument.
CREATE INDEX instrument_sources_instrument_active_idx
    ON instrument_sources (instrument_id, is_active);

-- Enforce the Phase 1 rule that each canonical instrument has at most one authoritative source.
CREATE UNIQUE INDEX instrument_sources_one_authoritative_idx
    ON instrument_sources (instrument_id)
    WHERE is_authoritative = TRUE;

-- Make all changes permanent only after every statement succeeds.
COMMIT;
