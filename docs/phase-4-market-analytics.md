# Phase 4 — Market Analytics Foundation

Phase 4 adds read-only analytics on top of stored daily market candles. The
analytics engine is provider-independent, calculations happen on request, and
the existing candle and live-monitor APIs remain unchanged.

For a detailed, beginner-friendly explanation of every metric, see the
[Analytics Metrics Guide](analytics-metrics-guide.md).

## API endpoints

### One instrument

```text
GET /api/v1/analytics/market?symbol=BTCUSDT&provider=binance&interval=1d&from=2026-08-01T00:00:00Z&to=2026-09-01T00:00:00Z
```

The response contains a range summary and one series item per returned daily
candle. Decimal analytics values are JSON strings; a value of `"0.125"`
represents a 12.5% ratio.

Series fields include:

- `period_return`
- `cumulative_return`
- `sma_7`, `sma_20`, and `sma_50`
- `volatility_20`
- `drawdown`

The summary includes total return, maximum drawdown, annualized 20-observation
volatility, first/last close, and the visible candle count.

### Comparison

```text
GET /api/v1/analytics/compare?symbols=BTCUSDT,ETHUSDT&provider=binance&interval=1d&from=2026-08-01T00:00:00Z&to=2026-09-01T00:00:00Z
```

Each instrument is normalized independently to 100 at its first available
close. Comparison is limited to eight canonical symbols using the same provider
and daily interval.

## Calculation rules

- Period return: `close[t] / close[t-1] - 1`
- Cumulative return: `close[t] / first_visible_close - 1`
- Moving average: arithmetic mean over 7, 20, or 50 candle observations
- Volatility: sample standard deviation of the latest 20 simple returns,
  annualized with `sqrt(252)`
- Drawdown: `close[t] / running_high - 1`
- Maximum drawdown: the lowest visible drawdown in the selected range

The API fetches up to 50 candles before `from` to warm rolling indicators. The
lookback candles are not returned, and the first visible period return remains
`null` because it is the beginning of the requested range. Missing calendar
days are not replaced with artificial zero-price candles.

Values remain `null` until their required observation window is available. An
empty range returns `summary: null` and `series: []`.

## Dashboard

The Analytics section is separate from the Market Data section. It provides
instrument and provider selection, a daily date range, optional comparison
selection, summary cards, moving-average and price charts, cumulative
performance, and drawdown.

Phase 4 intentionally does not add trading advice, signals, forecasts, machine
learning, personal-finance data, macro relationships, scheduled jobs, or
persisted derived-metric tables.
