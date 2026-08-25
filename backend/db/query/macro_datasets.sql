-- Phase 3.3 update: macro-dataset queries were added so the application can
-- discover CPI, repo-rate, and future macroeconomic series by stable code.

-- name: CreateMacroDataset :one
-- Creates a macro dataset definition and returns the stored metadata.
INSERT INTO macro_datasets (
    code,
    name,
    provider,
    metric,
    unit,
    frequency,
    observation_type,
    base_period,
    source_url,
    retrieved_at,
    metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING
    id,
    code,
    name,
    provider,
    metric,
    unit,
    frequency,
    observation_type,
    base_period,
    source_url,
    retrieved_at,
    is_active,
    metadata;

-- name: GetMacroDatasetByCode :one
-- Retrieves one dataset definition using MoneyPlant's stable application code.
SELECT
    id,
    code,
    name,
    provider,
    metric,
    unit,
    frequency,
    observation_type,
    base_period,
    source_url,
    retrieved_at,
    is_active,
    metadata
FROM macro_datasets
WHERE code = $1;

-- name: ListActiveMacroDatasets :many
-- Returns datasets available for new ingestion or dashboard selection.
SELECT
    id,
    code,
    name,
    provider,
    metric,
    unit,
    frequency,
    observation_type,
    base_period,
    source_url,
    retrieved_at,
    is_active,
    metadata
FROM macro_datasets
WHERE is_active = TRUE
ORDER BY code;
