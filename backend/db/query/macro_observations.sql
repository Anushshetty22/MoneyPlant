-- Phase 3.3 update: macro-observation queries were added so the application can
-- read and write dated CPI, repo-rate, and future macroeconomic values.

-- Phase 4.5 update: idempotent insert/update queries were added so CSV reseeding
-- can refresh an existing dataset/date without creating duplicate observations.

-- name: InsertMacroObservationIfAbsent :one
-- Inserts one observation only when its dataset/date key is new. When the key
-- already exists, PostgreSQL returns no row and the repository performs the
-- update path below. This keeps reseeding idempotent while allowing the caller
-- to count inserts and updates separately.
INSERT INTO macro_observations (
    macro_dataset_id,
    observed_on,
    value,
    source_retrieved_at,
    source_row_reference,
    metadata
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (macro_dataset_id, observed_on) DO NOTHING
RETURNING
    id,
    macro_dataset_id,
    observed_on,
    value,
    source_retrieved_at,
    source_row_reference,
    metadata;

-- name: UpdateMacroObservation :one
-- Refreshes the value and provenance for an existing dataset/date key. The
-- updated_at column changes automatically so the row records the reseed time.
UPDATE macro_observations
SET value = $3,
    source_retrieved_at = $4,
    source_row_reference = $5,
    metadata = $6,
    updated_at = NOW()
WHERE macro_dataset_id = $1
  AND observed_on = $2
RETURNING
    id,
    macro_dataset_id,
    observed_on,
    value,
    source_retrieved_at,
    source_row_reference,
    metadata;

-- name: CreateMacroObservation :one
-- Inserts one numeric observation and returns the stored row. The unique
-- dataset/date constraint prevents duplicate observations during reseeding.
INSERT INTO macro_observations (
    macro_dataset_id,
    observed_on,
    value,
    source_retrieved_at,
    source_row_reference,
    metadata
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING
    id,
    macro_dataset_id,
    observed_on,
    value,
    source_retrieved_at,
    source_row_reference,
    metadata;

-- name: ListMacroObservationsByDatasetCode :many
-- Returns observations for one dataset ordered chronologically. The join lets
-- callers use rbi_cpi_combined_yoy instead of an internal dataset ID.
SELECT
    macro_observations.id,
    macro_observations.macro_dataset_id,
    macro_observations.observed_on,
    macro_observations.value,
    macro_observations.source_retrieved_at,
    macro_observations.source_row_reference,
    macro_observations.metadata
FROM macro_observations
JOIN macro_datasets ON macro_datasets.id = macro_observations.macro_dataset_id
WHERE macro_datasets.code = $1
ORDER BY macro_observations.observed_on;

-- Phase 6.2 update: add a date-range query for dashboard requests. The range
-- is half-open, so the start date is included and the end date is excluded.
-- This matches the market-candle API behavior and makes adjacent requests safe.
-- name: ListMacroObservationsByDatasetCodeInRange :many
SELECT
    macro_observations.id,
    macro_observations.macro_dataset_id,
    macro_observations.observed_on,
    macro_observations.value,
    macro_observations.source_retrieved_at,
    macro_observations.source_row_reference,
    macro_observations.metadata
FROM macro_observations
JOIN macro_datasets ON macro_datasets.id = macro_observations.macro_dataset_id
WHERE macro_datasets.code = $1
  AND macro_observations.observed_on >= $2
  AND macro_observations.observed_on < $3
ORDER BY macro_observations.observed_on;
