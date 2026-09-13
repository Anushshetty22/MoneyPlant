# Phase 3.8 — Unified live monitor and status API

Phase 3.8 connects provider adapters to the same background monitoring and
status model. Binance symbols continue to use independent streams. Angel One
uses one multiplexed stream, while the API keeps one status record per Angel
One provider symbol.

## Configuration

Binance remains opt-in through the existing settings:

```env
LIVE_MONITOR_SYMBOLS=BTCUSDT,ETHUSDT
```

Angel One is enabled separately:

```env
ANGEL_ONE_LIVE_MONITOR_SYMBOLS=NIFTY50,SBIN,RELIANCE,TCS,INFY,HDFCBANK
```

The Angel One list uses MoneyPlant canonical symbols. At API startup, each
symbol is resolved through the active Angel One source mapping created by the
catalog refresh. The current provider symbol, token, and exchange segment are
then used to build one multiplexed subscription.

## Lifecycle and status

Every monitor has its own state:

```text
disabled → starting → running
                    ↘ reconnecting → running
                    ↘ stopped
                    ↘ error
```

The status registry keeps provider and provider-symbol keys separate, so a
failed Binance monitor does not change an Angel One monitor. The event counters
and the latest event timestamps belong to the corresponding provider symbol.

Persistence failures are reported in `last_persistence_error` and do not erase
the stream state. Stream, setup, and reconnect failures use the stream error
fields instead.

## API

The compatibility endpoint remains available:

```text
GET /api/v1/live/status
```

It returns one deterministic status record, which keeps existing clients
working. The Phase 3.8 endpoint returns all registered records:

```text
GET /api/v1/live/statuses
```

Example response shape:

```json
{
  "data": [
    {
      "provider": "angel_one",
      "canonical_symbol": "NIFTY50",
      "provider_symbol": "Nifty 50",
      "state": "running",
      "received": 12,
      "accepted": 12,
      "rejected": 0,
      "reconnects": 0,
      "last_persistence_error": null
    }
  ]
}
```

## Verification

From `backend/`:

```bash
go test ./internal/config
go test -run '^TestLiveMonitorStatusesEndpointReturnsIndependentProviderStates$' ./internal/httpapi
go test -race -run '^TestRunLiveMonitor' ./cmd/api
go test -run '^$' ./...
```
