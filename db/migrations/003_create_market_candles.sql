-- MoneyPlant Phase 1, migration 003
-- Purpose: store normalized historical OHLCV candles for each provider mapping.

-- Run this migration atomically so the candle table and its safeguards are created together.
BEGIN;

-- Store one normalized candle for one provider mapping, interval, and candle-open timestamp.
CREATE TABLE market_candles (
    -- Database-generated identity for the normalized candle row.
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    -- Preserve the exact provider mapping that supplied this candle.
    instrument_source_id BIGINT NOT NULL,

    -- Candle interval; Phase 1 ingestion initially uses 1d but the model supports common intervals.
    interval TEXT NOT NULL,

    -- UTC beginning of the provider candle interval.
    observed_at TIMESTAMPTZ NOT NULL,

    -- Provider close boundary, when the source supplies it.
    source_close_at TIMESTAMPTZ,

    -- Required normalized OHLCV values.
    open NUMERIC(30, 10) NOT NULL,
    high NUMERIC(30, 10) NOT NULL,
    low NUMERIC(30, 10) NOT NULL,
    close NUMERIC(30, 10) NOT NULL,
    volume NUMERIC(38, 18) NOT NULL,

    -- Optional Binance metrics; NULL means the provider did not supply the field.
    quote_volume NUMERIC(38, 18),
    trade_count BIGINT,
    taker_buy_volume NUMERIC(38, 18),
    taker_buy_quote_volume NUMERIC(38, 18),

    -- Preserve retrieval time separately from the market observation time.
    source_retrieved_at TIMESTAMPTZ NOT NULL,

    -- Audit timestamps for the normalized row and later upserts.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Prevent orphaned candles and preserve historical data if a source mapping is retired.
    CONSTRAINT market_candles_instrument_source_fk
        FOREIGN KEY (instrument_source_id)
        REFERENCES instrument_sources (id)
        ON DELETE RESTRICT,

    -- Restrict intervals to the common values supported by the Phase 1 model.
    CONSTRAINT market_candles_interval_allowed
        CHECK (interval IN ('1m', '5m', '15m', '30m', '1h', '4h', '1d', '1w')),

    -- Prevent negative prices or base-asset volume values.
    CONSTRAINT market_candles_ohlcv_non_negative
        CHECK (open >= 0 AND high >= 0 AND low >= 0 AND close >= 0 AND volume >= 0),

    -- Ensure the high contains both open and close prices.
    CONSTRAINT market_candles_high_contains_open_close
        CHECK (high >= GREATEST(open, close)),

    -- Ensure the low contains both open and close prices.
    CONSTRAINT market_candles_low_contains_open_close
        CHECK (low <= LEAST(open, close)),

    -- Ensure the candle's high is not below its low.
    CONSTRAINT market_candles_high_not_below_low
        CHECK (high >= low),

    -- Optional provider metrics must also be non-negative when supplied.
    CONSTRAINT market_candles_provider_metrics_non_negative
        CHECK (
            (quote_volume IS NULL OR quote_volume >= 0)
            AND (trade_count IS NULL OR trade_count >= 0)
            AND (taker_buy_volume IS NULL OR taker_buy_volume >= 0)
            AND (taker_buy_quote_volume IS NULL OR taker_buy_quote_volume >= 0)
        ),

    -- A provider close boundary cannot occur before the candle opens.
    CONSTRAINT market_candles_close_after_open
        CHECK (source_close_at IS NULL OR source_close_at >= observed_at),

    -- This key makes ingestion idempotent: the same candle can be safely upserted.
    CONSTRAINT market_candles_observation_unique
        UNIQUE (instrument_source_id, interval, observed_at)
);

-- Support chart-range queries ordered from the newest candle backward.
CREATE INDEX market_candles_chart_query_idx
    ON market_candles (instrument_source_id, interval, observed_at DESC);

-- Make all changes permanent only after every statement succeeds.
COMMIT;
