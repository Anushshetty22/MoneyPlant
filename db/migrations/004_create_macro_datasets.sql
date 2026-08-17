-- MoneyPlant Phase 1, migration 004
-- Purpose: define macroeconomic datasets before storing their observations.

-- Run this migration atomically so the dataset definition table is created completely.
BEGIN;

-- Store one stable definition for each macroeconomic series, such as CPI or repo rate.
CREATE TABLE macro_datasets (
    -- Database-generated identity for one macroeconomic dataset definition.
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- Stable application code, such as rbi_cpi_combined_yoy.
    code TEXT NOT NULL,

    -- Human-readable name shown in documentation and later in the dashboard.
    name TEXT NOT NULL,

    -- Source provider, initially rbi_dbie for the Phase 1 macro datasets.
    provider TEXT NOT NULL,

    -- Exact measure represented by this dataset, such as CPI (combined) or Policy repo.
    metric TEXT NOT NULL,

    -- Explicit unit so values such as 2.73 are not misinterpreted.
    unit TEXT NOT NULL,

    -- Frequency of the observations in this dataset.
    frequency TEXT NOT NULL,

    -- Explains what observed_on means for this series.
    observation_type TEXT NOT NULL,

    -- Optional index base period, such as a CPI base year.
    base_period TEXT,

    -- Official portal or publication location used for the dataset.
    source_url TEXT NOT NULL,

    -- When the metadata or seed source was reviewed.
    retrieved_at TIMESTAMPTZ NOT NULL,

    -- Inactive datasets remain available for historical queries but are excluded from new seeding.
    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    -- Flexible storage for source notes, export limitations, and additional definitions.
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,

    -- Audit timestamps for the dataset definition.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Prevent duplicate dataset definitions.
    CONSTRAINT macro_datasets_code_unique
        UNIQUE (code),

    -- Restrict frequency to the meanings supported by the Phase 1 model.
    CONSTRAINT macro_datasets_frequency_allowed
        CHECK (frequency IN ('monthly', 'daily', 'event')),

    -- Dataset definitions must explain their unit and observation frequency.
    CONSTRAINT macro_datasets_unit_present
        CHECK (unit <> ''),
    CONSTRAINT macro_datasets_frequency_present
        CHECK (frequency <> ''),

    -- The date meaning is required because CPI reference periods and policy dates differ.
    CONSTRAINT macro_datasets_observation_type_present
        CHECK (observation_type <> ''),

    -- Keep an invalid or blank source location out of the provenance record.
    CONSTRAINT macro_datasets_source_url_present
        CHECK (source_url <> '')
);

-- Speed up listing active datasets by source provider.
CREATE INDEX macro_datasets_provider_active_idx
    ON macro_datasets (provider, is_active);

-- Make all changes permanent only after every statement succeeds.
COMMIT;
