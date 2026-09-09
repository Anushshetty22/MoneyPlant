# Phase 3.2 Multiple Live Instruments

Phase 3.2 removes the one-symbol runtime limitation while keeping the Phase 2
single-symbol commands and endpoint behavior compatible.

## Configuration

The existing setting remains valid:

```bash
LIVE_MONITOR_SYMBOL=BTCUSDT
```

Multiple Binance symbols can now be configured with:

```bash
LIVE_MONITOR_SYMBOLS=BTCUSDT,ETHUSDT
```

Symbols are trimmed, normalized to uppercase, and rejected when empty,
duplicated, or containing unsupported characters. When both variables are set,
`LIVE_MONITOR_SYMBOLS` takes precedence. `LIVE_MONITOR_SYMBOL` is populated
with the first plural value for compatibility with existing code.

## Independent runtime behavior

Each configured provider-symbol pair receives:

- its own reconnecting stream;
- its own monitor goroutine;
- its own status store and counters;
- the shared thread-safe snapshot store keyed by provider and provider symbol.

Conceptually:

```text
BTCUSDT stream ──> BTCUSDT status ──┐
                                    ├─> shared snapshot store
ETHUSDT stream ──> ETHUSDT status ──┘
```

If one stream fails, its status becomes `error` after its retry policy is
exhausted. The other stream continues and keeps receiving events.

The snapshot endpoint keeps the old symbol filter:

```text
GET /api/v1/live/snapshots?symbol=BTCUSDT
```

It also accepts an optional provider filter for unambiguous future
multi-provider lookups:

```text
GET /api/v1/live/snapshots?provider=binance&symbol=BTCUSDT
```

The singular `/api/v1/live/status` endpoint continues to expose the first
configured monitor for compatibility. The registry already stores every
provider-symbol status; the plural status endpoint is scheduled for Phase 3.8.
