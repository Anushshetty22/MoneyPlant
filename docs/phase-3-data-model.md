# Phase 3.1 Unified Data Model

This document records the shared market-data vocabulary frozen before Angel
One integration. Phase 3.2 now uses that vocabulary for multi-symbol live
monitoring.

## Canonical instruments and provider identities

MoneyPlant uses one stable canonical identity for an instrument and keeps each
provider's representation in a separate source mapping.

| Concept | Example | Meaning |
|---|---|---|
| Canonical symbol | `SBIN` | MoneyPlant's stable symbol used by the dashboard and domain model |
| Provider | `yahoo` | External source that supplies the data |
| Provider symbol | `SBIN.NS` | Exact symbol sent to that provider |
| Provider instrument ID | Angel One token | Optional provider identifier; it is not a MoneyPlant primary key |

The canonical symbol must be uppercase and must not be sent to a provider by
assumption. A provider mapping resolves the canonical symbol to the exact
provider symbol and, when required, a provider token. Provider symbols and
tokens may change; the canonical identity does not.

The database representation is:

```text
instruments
    1 ─── many
instrument_sources
    1 ─── many
market_candles
```

The live contracts carry the same identity fields. Multi-symbol orchestration
uses one independent monitor per provider-symbol pair.

## Provider capabilities

Every historical or live adapter implements the common provider metadata
contract:

```go
ProviderName() string
Capabilities() ingestion.ProviderCapabilities
```

`ProviderCapabilities` declares:

- whether the adapter supports historical candles;
- whether it supports live events;
- which normalized candle intervals it supports.

Intervals describe historical candles. A live trade stream is event-based and
does not need an interval. Current declarations are:

| Adapter | Historical | Live | Historical intervals |
|---|---:|---:|---|
| Binance historical | yes | no | `1m`, `5m`, `15m`, `30m`, `1h`, `4h`, `1d`, `1w` |
| Binance live | no | yes | not applicable |
| Yahoo historical | yes | no | `1d` |
| Fixture historical | yes | no | all normalized intervals |
| Fixture live | no | yes | not applicable |
| Angel One | planned | planned | defined in its sub-phases |

This metadata lets a future provider registry reject unsupported work before a
network request is made.

## Shared live status vocabulary

Provider-aware monitors use these lifecycle states:

```text
disabled → starting → running
                  ↘ reconnecting → running
                  ↘ stopped
                  ↘ error
```

The common model identifies the provider, canonical symbol, provider symbol,
counters, last error, and update time. The existing `/api/v1/live/status`
endpoint remains compatible with Phase 2 and shows the first configured
monitor. The status registry already stores every provider-symbol status; Phase
3.8 will expose the plural status endpoint.

## Common error vocabulary

Provider and persistence failures use `MarketError` as the shared envelope.
The error includes:

- a stable code: `configuration`, `authentication`, `authorization`,
  `rate_limit`, `transport`, `decode`, `validation`, `unsupported`,
  `persistence`, or `unknown`;
- provider and symbol context;
- the operation that failed;
- a retryable hint;
- the wrapped provider-specific cause.

The retryable flag describes policy context; it does not cause an automatic
retry. Reconnect policy remains a separate responsibility.

## Compatibility boundary

Phase 3.1 did not add Angel One credentials, network calls, or token parsing.
Phase 3.2 adds multi-symbol lifecycle management without changing the existing
Binance and Yahoo adapter behavior.
