-- Phase 3.3 update: macro-observation queries were added so the application can
-- read and write dated CPI, repo-rate, and future macroeconomic values.

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
