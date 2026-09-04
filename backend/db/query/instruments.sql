-- Phase 3.3 update: instrument queries were moved into a dedicated SQL file
-- so sqlc can generate the corresponding type-safe Go methods.

-- name: CreateInstrument :one
-- Inserts one canonical instrument and returns the stored row, including the
-- database-generated ID and default values.
INSERT INTO instruments (
    canonical_symbol,
    name,
    asset_type,
    exchange,
    currency,
    metadata
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING
    id,
    canonical_symbol,
    name,
    asset_type,
    exchange,
    currency,
    is_active,
    metadata;

-- name: GetInstrumentByCanonicalSymbol :one
-- Retrieves one instrument. The database UNIQUE constraint guarantees that
-- this lookup returns at most one row.
SELECT
    id,
    canonical_symbol,
    name,
    asset_type,
    exchange,
    currency,
    is_active,
    metadata
FROM instruments
WHERE canonical_symbol = $1;

-- Phase 6.2 update: add the first read-only API query. Returning only active
-- instruments keeps the public catalog focused on symbols currently available
-- for ingestion and charting.
-- name: ListActiveInstruments :many
SELECT
    id,
    canonical_symbol,
    name,
    asset_type,
    exchange,
    currency,
    is_active,
    metadata
FROM instruments
WHERE is_active = true
ORDER BY canonical_symbol;
