# MoneyPlant Analytics Metrics Guide

This document explains the metrics currently calculated by MoneyPlant in
simple terms. It is intended both for learning and for explaining the project
to someone else.

## What the analytics engine does

MoneyPlant receives normalized daily candles. A candle summarizes one trading
observation:

| Field | Meaning |
| --- | --- |
| `observed_at` | The time represented by the candle |
| `close` | The final traded price during that observation |

The engine orders the candles by time and calculates measurements from the
closing prices. It does not make predictions, give buy/sell advice, or claim
that a metric guarantees future performance.

The current engine supports daily candles and uses exact decimal arithmetic
internally. Results are returned as decimal strings so financial values are not
silently changed by binary floating-point rounding.

## A small example

Suppose an instrument has these closing prices:

| Day | Close |
| --- | ---: |
| 1 | 100 |
| 2 | 105 |
| 3 | 102 |
| 4 | 110 |

From this small series we can say:

- Day 2 returned `5%` because `105 / 100 - 1 = 0.05`.
- Day 3 returned about `-2.86%` because `102 / 105 - 1` is negative.
- The total return from Day 1 to Day 4 is `10%` because `110 / 100 - 1 = 0.10`.
- The highest close so far is 105 after Day 2, so Day 3 is below that high. That
  decline is drawdown.

The API represents `5%` as the string `"0.05"`. The frontend converts that
ratio into a human-readable percentage.

## 1. Period return

### What it means

Period return measures how much the closing price changed from the previous
observation to the current observation.

### Formula

```text
period return = current close / previous close - 1
```

### Example

If yesterday's close was 100 and today's close is 105:

```text
105 / 100 - 1 = 0.05 = 5%
```

If today's close is 95:

```text
95 / 100 - 1 = -0.05 = -5%
```

### Why it is useful

Period return shows the movement from one observation to the next. It is the
basic input for volatility and helps identify positive and negative daily
movements.

### Important limitation

The first returned point has no previous point inside the requested range, so
its period return is `null`. It is not treated as zero because that would
invent a movement that was never observed.

## 2. Cumulative return and total return

### What it means

Cumulative return measures performance from the beginning of the selected
range to each later observation. The summary's total return is the final
cumulative return in that range.

### Formula

```text
cumulative return = current close / first close - 1
```

### Example

If the first close is 100 and the current close is 110:

```text
110 / 100 - 1 = 0.10 = 10%
```

### Why it is useful

It answers the simple question: “How much would the price have changed over
this selected period?” It is more useful for understanding the whole period
than looking at only the last daily movement.

### Important limitation

This is price return only. It does not include dividends, interest, fees,
taxes, funding costs, or slippage.

## 3. Simple moving averages: SMA-7, SMA-20, and SMA-50

### What it means

A simple moving average is the arithmetic average of the most recent closing
prices. “Moving” means that the window moves forward one observation at a time.

### Formula

```text
SMA-N = sum of the latest N closing prices / N
```

MoneyPlant currently calculates:

- SMA-7: short-term price direction
- SMA-20: medium-term price direction
- SMA-50: longer-term price direction

### Example

For three closes of 100, 105, and 110:

```text
SMA-3 = (100 + 105 + 110) / 3 = 105
```

### Why it is useful

Moving averages smooth noisy price movements. They help a user see whether
the recent price is generally rising, falling, or moving sideways.

For example, if the current close is above the SMA-20, the recent price is
higher than its 20-observation average. That is descriptive information, not a
trading signal by itself.

### Warm-up behavior

SMA-7 is `null` until seven observations exist. SMA-20 needs twenty, and SMA-50
needs fifty. The system never fills missing history with fake zero-price
candles.

## 4. Annualized 20-observation volatility

### What it means

Volatility measures how widely returns vary. Higher volatility means the price
has moved less consistently; lower volatility means returns have been more
stable over the measured window.

MoneyPlant uses the sample standard deviation of the latest 20 simple returns
and annualizes it using 252 market sessions per year.

### Formula

```text
volatility = sample standard deviation of the latest 20 returns
annualized volatility = volatility * sqrt(252)
```

The sample standard deviation divides by `N - 1`, not `N`, because the 20
observations are treated as a sample of possible future returns.

### Why it is useful

Volatility helps compare how turbulent instruments or periods have been. For
example, an instrument with annualized volatility `0.30` has an observed
annualized variability of about `30%` under this measurement convention.

It does not say whether the price will rise or fall. It measures size and
consistency of movement, not direction.

### Warm-up behavior

The engine needs 20 returns, which requires at least 21 ordered closing prices.
Until then, volatility is `null`.

### Important limitation

The factor 252 is a convention for annualizing daily market observations. It
is appropriate for daily market analytics, but it is not a prediction and may
not be appropriate for every asset or future interval.

## 5. Drawdown

### What it means

Drawdown measures how far the current close is below the highest close reached
so far in the selected series.

### Formula

```text
drawdown = current close / running high - 1
```

The running high is the highest close observed from the beginning of the
calculation window through the current point.

### Example

If the highest previous close is 120 and the current close is 108:

```text
108 / 120 - 1 = -0.10 = -10%
```

The asset is 10% below its running high.

### Why it is useful

Drawdown describes downside from a previous high. It is often easier to
understand risk with drawdown than with return alone: an asset can finish a
period positively while still having suffered a large temporary decline.

At a new high, drawdown is `0`. Drawdown is normally zero or negative.

## 6. Maximum drawdown

### What it means

Maximum drawdown is the worst drawdown reached during the selected range.

### Example

If the drawdown series is:

```text
0%, -4%, -2%, -12%, -6%
```

the maximum drawdown is `-12%`.

### Why it is useful

It summarizes the largest peak-to-trough decline visible in the selected
period. It helps answer: “What was the deepest decline an investor would have
experienced from a previous observed high during this range?”

### Important limitation

The result depends on the selected date range. A longer history can reveal a
larger drawdown than a shorter window.

## 7. Normalized comparison index

### What it means

The comparison endpoint converts every instrument's first available close to
100. Every later value shows what that starting 100 would have become if it
followed that instrument's price performance.

### Formula

```text
normalized value = current close / first close * 100
```

### Example

If Instrument A moves from 100 to 110:

```text
110 / 100 * 100 = 110
```

If Instrument B moves from 200 to 220:

```text
220 / 200 * 100 = 110
```

Both instruments gained 10%, even though their actual prices are different.

### Why it is useful

Raw prices cannot be compared fairly when instruments start at different price
levels. Normalization puts them on the same visual scale so relative
performance can be compared.

### Important limitation

Comparison is meaningful only when instruments use the same provider, interval,
and comparable date window. The current implementation normalizes each
instrument using its first available candle; it does not invent a value when
an instrument has no data.

## Data rules behind the metrics

The analytics engine also enforces several safety rules:

- Candles are sorted chronologically before calculation.
- Duplicate timestamps are rejected.
- Close prices must be positive decimal values.
- Missing calendar days are not replaced with zero-price candles.
- Rolling indicators remain `null` during their warm-up period.
- Up to 50 earlier candles may be fetched internally to warm indicators, but
  only the requested date range is returned by the API.
- Empty ranges return no fabricated summary or series.

## How to explain MoneyPlant in one minute

“MoneyPlant stores daily market candles from different providers and converts
them into a common format. Its analytics engine calculates descriptive metrics
on demand: returns show price change, moving averages smooth the price,
volatility measures how much returns vary, drawdown measures decline from a
previous high, and normalized comparison puts different instruments on the
same starting scale. The engine is provider-independent and returns exact
decimal values. It describes historical market behavior; it does not predict
prices or give trading advice.”

## What these metrics do not provide

These metrics currently do not include:

- dividends or total-return adjustments
- transaction costs, taxes, or slippage
- fundamental company analysis
- portfolio-level risk
- forecasts or machine learning
- buy/sell recommendations
- real-time 1-minute analytics
- personal-finance or spending analysis

Those are separate future product decisions and should not be implied by the
current analytics output.
