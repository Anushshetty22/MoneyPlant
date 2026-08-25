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
