-- Phase 3.3 update: ingestion-run queries were added so batch jobs can record
-- their lifecycle, row counts, scope, and failures through sqlc.

-- name: CreateIngestionRun :one
-- Starts one ingestion attempt in the running state and returns its audit row.
INSERT INTO ingestion_runs (
    run_type,
    provider,
    status,
    started_at,
    requested_from,
    requested_to,
    scope
)
VALUES ($1, $2, 'running', $3, $4, $5, $6)
RETURNING
    id,
    run_type,
    provider,
    status,
    started_at,
    completed_at,
    requested_from,
    requested_to,
    rows_received,
    rows_inserted,
    rows_updated,
    rows_rejected,
    error_message,
    scope,
    created_at;

-- name: CompleteIngestionRun :one
-- Closes a run with its final status, completion timestamp, counts, and optional error.
-- The database completion constraint ensures the resulting status is consistent.
UPDATE ingestion_runs
SET
    status = $2,
    completed_at = $3,
    rows_received = $4,
    rows_inserted = $5,
    rows_updated = $6,
    rows_rejected = $7,
    error_message = $8
WHERE id = $1
RETURNING
    id,
    run_type,
    provider,
    status,
    started_at,
    completed_at,
    requested_from,
    requested_to,
    rows_received,
    rows_inserted,
    rows_updated,
    rows_rejected,
    error_message,
    scope,
    created_at;

-- name: ListRecentIngestionRunsByProvider :many
-- Returns recent audit records for one provider, newest runs first.
SELECT
    id,
    run_type,
    provider,
    status,
    started_at,
    completed_at,
    requested_from,
    requested_to,
    rows_received,
    rows_inserted,
    rows_updated,
    rows_rejected,
    error_message,
    scope,
    created_at
FROM ingestion_runs
WHERE provider = $1
ORDER BY started_at DESC
LIMIT $2;
