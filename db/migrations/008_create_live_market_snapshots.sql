-- MoneyPlant Phase 2.12
-- Purpose: retain one restart-safe latest live trade per provider symbol.

BEGIN;

-- This table stores the latest value, not the complete high-volume trade stream.
-- Raw live ticks remain outside PostgreSQL until a separate retention design is
-- chosen deliberately.
CREATE TABLE live_market_snapshots (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_symbol TEXT NOT NULL,
    event_type TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    price NUMERIC(30, 10) NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL,
    source_received_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT live_market_snapshots_provider_present CHECK (provider <> ''),
    CONSTRAINT live_market_snapshots_symbol_present CHECK (provider_symbol <> ''),
    CONSTRAINT live_market_snapshots_event_type_allowed CHECK (event_type = 'trade'),
    CONSTRAINT live_market_snapshots_price_positive CHECK (price > 0),
    CONSTRAINT live_market_snapshots_quantity_positive CHECK (quantity > 0),
    CONSTRAINT live_market_snapshots_received_after_observed CHECK (source_received_at >= observed_at),
    CONSTRAINT live_market_snapshots_provider_symbol_unique UNIQUE (provider, provider_symbol)
);

CREATE INDEX live_market_snapshots_updated_idx
    ON live_market_snapshots (updated_at DESC);

COMMIT;
