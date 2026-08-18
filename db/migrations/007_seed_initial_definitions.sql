-- MoneyPlant Phase 1, migration 007
-- Purpose: seed initial instruments, provider mappings, and macro dataset definitions.
-- This migration contains no API keys, passwords, or other secrets.

-- Run the complete seed as one transaction so related definitions are inserted together.
BEGIN;

-- Seed the initial crypto instrument used by the Binance fixture and Phase 1 pipeline.
INSERT INTO instruments (
    canonical_symbol,
    name,
    asset_type,
    exchange,
    currency,
    metadata
)
VALUES (
    'BTCUSDT',
    'Bitcoin / Tether',
    'crypto',
    'Binance',
    'USDT',
    '{"price_policy": "unadjusted"}'::JSONB
)
ON CONFLICT (canonical_symbol) DO NOTHING;

-- Seed the initial Indian equity used by the Yahoo Finance fallback fixture.
INSERT INTO instruments (
    canonical_symbol,
    name,
    asset_type,
    exchange,
    currency,
    metadata
)
VALUES (
    'SBIN',
    'State Bank of India',
    'equity',
    'NSE',
    'INR',
    '{"price_policy": "unadjusted"}'::JSONB
)
ON CONFLICT (canonical_symbol) DO NOTHING;

-- Map BTCUSDT to Binance as its authoritative market-data source.
INSERT INTO instrument_sources (
    instrument_id,
    provider,
    provider_symbol,
    is_authoritative,
    metadata
)
SELECT
    id,
    'binance',
    'BTCUSDT',
    TRUE,
    '{"intervals": ["1d"]}'::JSONB
FROM instruments
WHERE canonical_symbol = 'BTCUSDT'
ON CONFLICT (provider, provider_symbol) DO NOTHING;

-- Map SBIN to Yahoo Finance as the initial fallback source; Angel One is added later when its live adapter is implemented.
INSERT INTO instrument_sources (
    instrument_id,
    provider,
    provider_symbol,
    is_authoritative,
    metadata
)
SELECT
    id,
    'yahoo',
    'SBIN.NS',
    FALSE,
    '{"intervals": ["1d"], "role": "fallback"}'::JSONB
FROM instruments
WHERE canonical_symbol = 'SBIN'
ON CONFLICT (provider, provider_symbol) DO NOTHING;

-- Define the headline combined CPI inflation series as a monthly percentage rate.
INSERT INTO macro_datasets (
    code,
    name,
    provider,
    metric,
    unit,
    frequency,
    observation_type,
    source_url,
    retrieved_at,
    metadata
)
VALUES (
    'rbi_cpi_combined_yoy',
    'CPI inflation (combined)',
    'rbi_dbie',
    'CPI (combined)',
    'percent',
    'monthly',
    'reference_period',
    'https://dbie.rbihub.in/',
    '2026-08-11 00:00:00+05:30',
    '{"dashboard_label": "CPI (combined)", "seed_status": "definition_only"}'::JSONB
)
ON CONFLICT (code) DO NOTHING;

-- Define the RBI policy repo-rate series as a percentage rate with a documented date meaning.
INSERT INTO macro_datasets (
    code,
    name,
    provider,
    metric,
    unit,
    frequency,
    observation_type,
    source_url,
    retrieved_at,
    metadata
)
VALUES (
    'rbi_policy_repo_rate',
    'RBI policy repo rate',
    'rbi_dbie',
    'Policy repo',
    'percent',
    'monthly',
    'reference_period',
    'https://dbie.rbihub.in/',
    '2026-08-11 00:00:00+05:30',
    '{"dashboard_label": "Policy repo", "seed_status": "definition_only", "note": "Dashboard values are monthly snapshots; event-date semantics will be confirmed when the downloadable seed is selected."}'::JSONB
)
ON CONFLICT (code) DO NOTHING;

-- Make all seed changes permanent only after every definition has been processed successfully.
COMMIT;
