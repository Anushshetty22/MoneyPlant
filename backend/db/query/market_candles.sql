-- Phase 3.3 update: market-candle queries were added so the ingestion and API
-- layers can write and read normalized OHLCV observations through sqlc.

-- name: CreateMarketCandle :one
-- Inserts one candle and returns the stored row. The database constraints validate
-- OHLCV relationships, non-negative metrics, timestamps, and duplicate protection.
INSERT INTO market_candles (
    instrument_source_id,
    interval,
    observed_at,
    source_close_at,
    open,
    high,
    low,
    close,
    volume,
    quote_volume,
    trade_count,
    taker_buy_volume,
    taker_buy_quote_volume,
    source_retrieved_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING
    id,
    instrument_source_id,
    interval,
    observed_at,
    source_close_at,
    open,
    high,
    low,
    close,
    volume,
    quote_volume,
    trade_count,
    taker_buy_volume,
    taker_buy_quote_volume,
    source_retrieved_at;

-- name: ListMarketCandlesByCanonicalSymbol :many
-- Returns candles for one canonical instrument, provider, interval, and UTC
-- half-open time range. The join preserves the provider identity while allowing
-- callers to use stable application-level symbols.
SELECT
    market_candles.id,
    market_candles.instrument_source_id,
    market_candles.interval,
    market_candles.observed_at,
    market_candles.source_close_at,
    market_candles.open,
    market_candles.high,
    market_candles.low,
    market_candles.close,
    market_candles.volume,
    market_candles.quote_volume,
    market_candles.trade_count,
    market_candles.taker_buy_volume,
    market_candles.taker_buy_quote_volume,
    market_candles.source_retrieved_at
FROM market_candles
JOIN instrument_sources
    ON instrument_sources.id = market_candles.instrument_source_id
JOIN instruments
    ON instruments.id = instrument_sources.instrument_id
WHERE instruments.canonical_symbol = $1
  AND instrument_sources.provider = $2
  AND market_candles.interval = $3
  AND market_candles.observed_at >= $4
  AND market_candles.observed_at < $5
ORDER BY market_candles.observed_at;
