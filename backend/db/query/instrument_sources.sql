-- Phase 3.3 update: provider-mapping queries were added so the application can
-- resolve a canonical instrument to Binance, Yahoo Finance, or Angel One IDs.

-- name: CreateInstrumentSource :one
-- Creates one provider mapping and returns the stored mapping. The foreign-key
-- constraint ensures that the canonical instrument already exists.
INSERT INTO instrument_sources (
    instrument_id,
    provider,
    provider_symbol,
    provider_instrument_id,
    is_authoritative,
    metadata
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING
    id,
    instrument_id,
    provider,
    provider_symbol,
    provider_instrument_id,
    is_authoritative,
    is_active,
    metadata;

-- name: UpsertInstrumentSource :one
-- Refreshes a provider mapping from the latest instrument master. The provider
-- symbol is the stable natural key for the catalog row; the token is replaced
-- whenever Angel One publishes a new value.
INSERT INTO instrument_sources (
    instrument_id,
    provider,
    provider_symbol,
    provider_instrument_id,
    is_authoritative,
    metadata
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (provider, provider_symbol) DO UPDATE SET
    instrument_id = EXCLUDED.instrument_id,
    provider_instrument_id = EXCLUDED.provider_instrument_id,
    is_authoritative = EXCLUDED.is_authoritative,
    is_active = TRUE,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING
    id,
    instrument_id,
    provider,
    provider_symbol,
    provider_instrument_id,
    is_authoritative,
    is_active,
    metadata;

-- name: ListInstrumentSourcesByCanonicalSymbol :many
-- Returns every active and inactive provider mapping for one canonical symbol.
-- Joining through instruments lets callers use MoneyPlant's stable symbol instead
-- of knowing the internal instrument ID.
SELECT
    instrument_sources.id,
    instrument_sources.instrument_id,
    instrument_sources.provider,
    instrument_sources.provider_symbol,
    instrument_sources.provider_instrument_id,
    instrument_sources.is_authoritative,
    instrument_sources.is_active,
    instrument_sources.metadata
FROM instrument_sources
JOIN instruments ON instruments.id = instrument_sources.instrument_id
WHERE instruments.canonical_symbol = $1
ORDER BY instrument_sources.is_authoritative DESC, instrument_sources.provider;

-- name: GetAuthoritativeInstrumentSource :one
-- Retrieves the preferred provider mapping for one canonical symbol. The partial
-- unique index guarantees that an instrument has at most one authoritative source.
SELECT
    instrument_sources.id,
    instrument_sources.instrument_id,
    instrument_sources.provider,
    instrument_sources.provider_symbol,
    instrument_sources.provider_instrument_id,
    instrument_sources.is_authoritative,
    instrument_sources.is_active,
    instrument_sources.metadata
FROM instrument_sources
JOIN instruments ON instruments.id = instrument_sources.instrument_id
WHERE instruments.canonical_symbol = $1
  AND instrument_sources.is_authoritative = TRUE
  AND instrument_sources.is_active = TRUE;
