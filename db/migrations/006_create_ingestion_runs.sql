-- MoneyPlant Phase 1, migration 006
-- Purpose: record the status, scope, counts, and errors for each ingestion attempt.

-- Run this migration atomically so the audit table and its safeguards are created together.
BEGIN;

-- Store one operational record for each market-data, macro-seed, or migration attempt.
CREATE TABLE ingestion_runs (
    -- Database-generated identity used in logs and troubleshooting.
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- Type of operation represented by this audit record.
    run_type TEXT NOT NULL,

    -- Provider or source family used by the operation.
    provider TEXT NOT NULL,

    -- Current lifecycle state of the operation.
    status TEXT NOT NULL,

    -- Time at which the operation began; defaults to the database clock.
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- End time is populated when a non-running operation finishes.
    completed_at TIMESTAMPTZ,

    -- Optional requested market range; macro runs can leave these columns NULL.
    requested_from TIMESTAMPTZ,
    requested_to TIMESTAMPTZ,

    -- Source, insertion, update, and validation counts for operational reporting.
    rows_received BIGINT NOT NULL DEFAULT 0,
    rows_inserted BIGINT NOT NULL DEFAULT 0,
    rows_updated BIGINT NOT NULL DEFAULT 0,
    rows_rejected BIGINT NOT NULL DEFAULT 0,

    -- Human-readable summary of a failure or partial-load condition.
    error_message TEXT,

    -- Symbols, datasets, intervals, and command options included in the run.
    scope JSONB NOT NULL DEFAULT '{}'::JSONB,

    -- Audit-row creation timestamp.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Restrict the operation type to the Phase 1 commands.
    CONSTRAINT ingestion_runs_run_type_allowed
        CHECK (run_type IN ('market_api', 'macro_seed', 'migration')),

    -- Provider is required for clear audit history.
    CONSTRAINT ingestion_runs_provider_present
        CHECK (provider <> ''),

    -- Restrict lifecycle status to the values handled by the ingestion layer.
    CONSTRAINT ingestion_runs_status_allowed
        CHECK (status IN ('running', 'succeeded', 'partial', 'failed')),

    -- Every recorded count must be zero or greater.
    CONSTRAINT ingestion_runs_counts_non_negative
        CHECK (
            rows_received >= 0
            AND rows_inserted >= 0
            AND rows_updated >= 0
            AND rows_rejected >= 0
        ),

    -- Running attempts have no completion time; all other states must be complete.
    CONSTRAINT ingestion_runs_completion_consistent
        CHECK (
            (status = 'running' AND completed_at IS NULL)
            OR (status <> 'running' AND completed_at IS NOT NULL)
        ),

    -- A requested range cannot end before it begins.
    CONSTRAINT ingestion_runs_requested_range_valid
        CHECK (
            requested_from IS NULL
            OR requested_to IS NULL
            OR requested_to >= requested_from
        )
);

-- Support recent-run history for one provider.
CREATE INDEX ingestion_runs_provider_started_idx
    ON ingestion_runs (provider, started_at DESC);

-- Support operational views of recent failures and incomplete work.
CREATE INDEX ingestion_runs_status_started_idx
    ON ingestion_runs (status, started_at DESC);

-- Make all changes permanent only after every statement succeeds.
COMMIT;
