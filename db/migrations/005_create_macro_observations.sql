-- MoneyPlant Phase 1, migration 005
-- Purpose: store dated numeric observations for each defined macroeconomic dataset.

-- Run this migration atomically so the observation table and its safeguards are created together.
BEGIN;

-- Store one CPI, repo-rate, or other macro value for one dataset and one reference date.
CREATE TABLE macro_observations (
    -- Database-generated identity for one macroeconomic observation.
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- Link the observation to its metric, unit, frequency, and source definition.
    macro_dataset_id BIGINT NOT NULL,

    -- SQL DATE is intentional: this is a reference period or policy event date, not a market timestamp.
    observed_on DATE NOT NULL,

    -- Exact numeric value; negative values are allowed for metrics such as deflation.
    value NUMERIC(20, 8) NOT NULL,

    -- Preserve when the source value was retrieved or reviewed.
    source_retrieved_at TIMESTAMPTZ NOT NULL,

    -- Optional row, publication, or export reference from the source.
    source_row_reference TEXT,

    -- Flexible storage for revisions, missing-value notes, and date interpretation details.
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,

    -- Audit timestamps for the observation and later upserts.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Prevent orphaned observations and preserve historical data if a dataset is retired.
    CONSTRAINT macro_observations_dataset_fk
        FOREIGN KEY (macro_dataset_id)
        REFERENCES macro_datasets (id)
        ON DELETE RESTRICT,

    -- One dataset/date pair represents one observation; this makes reseeding idempotent.
    CONSTRAINT macro_observations_dataset_date_unique
        UNIQUE (macro_dataset_id, observed_on)
);

-- Support time-series queries for one macro dataset from the newest date backward.
CREATE INDEX macro_observations_dataset_date_idx
    ON macro_observations (macro_dataset_id, observed_on DESC);

-- Make all changes permanent only after every statement succeeds.
COMMIT;
