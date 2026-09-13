# Phase 3.9 — Latest snapshots and one-minute candles

Phase 3.9 stores useful live data without turning every provider tick into a
database row.

## Data flow

```text
live event
   ├── latest snapshot (one row per provider symbol)
   └── minute aggregator (one OHLCV row per source and UTC minute)
```

The latest snapshot remains available for fast API reads and restart recovery.
The minute aggregator keeps only compact rollup state: open, high, low, close,
volume, and trade count. The existing `market_candles` upsert stores the result
with interval `1m`.

## Candle rules

- The bucket is the UTC minute containing `ObservedAt`.
- Open and close are selected by event timestamp, so normal out-of-order events
  do not change the candle incorrectly.
- High and low are exact decimal comparisons.
- Volume is the exact sum of trade quantities.
- Trade count is the number of accepted events in the bucket.
- A late event can revise a recently flushed bucket and update the same natural
  database key. Clean old buckets are eventually removed from the in-memory
  late-event buffer after a short grace window.
- The active bucket is flushed when the monitor shuts down.

No raw live tick is archived. Repeated updates use the existing unique key:

```text
(instrument_source_id, interval, observed_at)
```

## Persistence and restart

Snapshots and candles use the shared PostgreSQL pool. Snapshot persistence
continues to be throttled by the existing live-monitor interval. Candle
rollups are written when a minute closes and the final active rollup is written
with the bounded shutdown persistence context. Snapshot restoration still runs
before live monitors start.

## Verification

From `backend/`:

```bash
go test -run '^TestLiveMinuteCandleAggregator' ./internal/ingestion -v
go test -run '^TestRunLiveMonitor' ./cmd/api -v
go test -race -run '^TestLiveMinuteCandleAggregator|^TestRunLiveMonitor' ./internal/ingestion ./cmd/api
go test -run '^$' ./...
```
