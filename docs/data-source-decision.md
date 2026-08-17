# MoneyPlant Phase 1 Data Sources

Status: source plan documented; Binance sample collected; Angel One sample deferred by decision.

This document explains what data MoneyPlant will use in Phase 1, how we will collect it, which values we will retain, and why each value matters. It is a design and research document, not proof that the sources have already been downloaded.

## 1. Phase 1 collection strategy

MoneyPlant will collect four categories of data:

1. Indian equities and indices from Angel One SmartAPI.
2. Cryptocurrency candles from Binance public market data.
3. NSE end-of-day fallback data from Yahoo Finance.
4. Indian macroeconomic observations from RBI's Database on Indian Economy (DBIE).

The initial implementation uses daily data for a small universe. After the first pipeline is reliable, we can add more symbols, shorter intervals, and more macroeconomic series.

## 2. Source summary

| Category | Source | Collection method | Phase 1 status |
|---|---|---|---|
| Indian equities/indexes | Angel One SmartAPI | Authenticated HTTP POST requests from Go | Planned; requires local credentials |
| Crypto OHLCV | Binance public market-data API | Unauthenticated HTTP GET requests from Go | Planned |
| NSE EOD fallback | Yahoo Finance chart endpoint | HTTP GET fallback from Go; use only when explicitly selected | Planned; endpoint must be smoke-tested |
| CPI inflation | RBI DBIE portal | Export selected series to reviewed CSV, then seed locally | Planned; portal workflow must be performed |
| RBI policy rate | RBI DBIE portal | Export selected series to reviewed CSV, then seed locally | Planned; portal workflow must be performed |

## 3. Data-source decisions

### 3.1 Angel One SmartAPI: Indian equities and indices

#### Why this source

Angel One is the primary Indian-market source in the project brief. It provides historical candle data and an instrument master containing tradable symbols and tokens. The instrument master is important because symbol tokens can change or should not be assumed from memory.

#### How we will collect it

1. Keep credentials in the local `.env` file only.
2. Authenticate and obtain the session authorization token.
3. Download or query the instrument master.
4. Resolve each configured symbol to its exchange and `symboltoken`.
5. Send authenticated POST requests to:

   `https://apiconnect.angelone.in/rest/secure/angelbroking/historical/v1/getCandleData`

6. Request `ONE_DAY` candles in date windows accepted by the API.
7. Normalize each returned array into the common MoneyPlant candle model.
8. Store the provider name and original symbol token with every normalized record.

The official documentation describes the request fields as exchange, symbol token, interval, from-date, and to-date. It describes each returned candle as timestamp, open, high, low, close, and volume. It also documents a maximum of 2,000 days for the daily interval. [Angel One SmartAPI documentation](https://smartapi.angelone.in/docs/Instruments)

#### Initial instruments

- NIFTY 50 index
- `SBIN-EQ`
- `RELIANCE-EQ`
- `TCS-EQ`
- `INFY-EQ`
- `HDFCBANK-EQ`

The exact exchange and token will be resolved from the instrument master during collection.

#### Values we will take from Angel One

| Source value | Normalized value | Why we need it |
|---|---|---|
| Candle timestamp | `observed_at` | Time axis for charts and queries |
| Open | `open` | Starting price for the daily trading period |
| High | `high` | Daily maximum and validation of the candle range |
| Low | `low` | Daily minimum and validation of the candle range |
| Close | `close` | Main value for trends, returns, and charts |
| Volume | `volume` | Activity and liquidity analysis |
| Exchange | `exchange` | Identifies the market segment |
| Symbol token | `provider_instrument_id` | Reproducibility and provider-level identity |
| Trading symbol | `provider_symbol` | Human-readable provider identifier |

We will not collect orders, holdings, account balances, or live WebSocket ticks in Phase 1.

### 3.2 Binance: cryptocurrency OHLCV

#### Why this source

Binance provides public spot-market historical klines without an API key. This gives us a credential-free source for learning HTTP ingestion, pagination, response parsing, and idempotent candle storage.

#### How we will collect it

1. Use the public market-data base URL recommended by Binance:

   `https://data-api.binance.vision`

2. Call:

   `GET /api/v3/klines`

3. Send `symbol`, `interval=1d`, `startTime`, `endTime`, and `limit` parameters.
4. Split large date ranges into multiple requests.
5. Parse the positional array response.
6. Normalize timestamps from milliseconds to UTC.
7. Upsert candles using symbol, interval, and candle-open time as the natural uniqueness key.

The Binance documentation identifies `data-api.binance.vision` for public market data and documents the Spot REST API. [Binance Spot REST API documentation](https://developers.binance.com/en/docs/binance-spot-api-docs/rest-api)

#### Initial pairs

- `BTCUSDT`
- `ETHUSDT`
- `BNBUSDT`

#### Values we will take from Binance

| Array position | Normalized value | Why we need it |
|---|---|---|
| 0: Open time | `observed_at` | Candle identity and chart timestamp |
| 1: Open price | `open` | Starting price |
| 2: High price | `high` | Maximum price and validation |
| 3: Low price | `low` | Minimum price and validation |
| 4: Close price | `close` | Main trend value |
| 5: Base-asset volume | `volume` | Trading activity in the crypto asset |
| 6: Close time | `source_close_at` | Source boundary and completeness checks |
| 7: Quote-asset volume | `quote_volume` | Optional value for comparing turnover in USDT |
| 8: Number of trades | `trade_count` | Activity-density metric |
| 9: Taker-buy base volume | `taker_buy_volume` | Optional buy-side activity metric |
| 10: Taker-buy quote volume | `taker_buy_quote_volume` | Optional buy-side turnover metric |

For the first normalized warehouse table, open, high, low, close, volume, and source metadata are required. The remaining Binance-specific values may be stored in a provider-details table or deferred until the base pipeline works.

### 3.3 Yahoo Finance: NSE EOD fallback

#### Why this source

Yahoo Finance is not the authoritative Indian-market source. It is a fallback for historical daily data when Angel One is unavailable or credentials are not configured.

#### How we will collect it

1. Convert the configured NSE symbol to Yahoo's `.NS` symbol.
2. Request the historical chart data for a date range and daily interval.
3. Parse the timestamp array and quote arrays.
4. Use Yahoo data only when the ingestion command explicitly selects the fallback or the primary provider fails according to a defined policy.
5. Store `source_provider=yahoo` so records are never confused with Angel One records.

The Yahoo chart endpoint and usage terms must be smoke-tested before implementation because Yahoo does not provide the same stable developer contract as Angel One or Binance. The `yfinance` documentation is useful for understanding historical download parameters, but it is not an official Yahoo developer API. [yfinance historical download documentation](https://ranaroussi.github.io/yfinance/reference/api/yfinance.download.html)

#### Initial fallback symbols

- `SBIN.NS`
- `RELIANCE.NS`
- `TCS.NS`
- `INFY.NS`
- `HDFCBANK.NS`

#### Values we will take from Yahoo

| Source value | Normalized value | Why we need it |
|---|---|---|
| Unix timestamp | `observed_at` | Candle date and chart axis |
| Open | `open` | Starting daily price |
| High | `high` | Daily maximum |
| Low | `low` | Daily minimum |
| Close | `close` | Main trend value |
| Volume | `volume` | Trading activity |
| Ticker | `provider_symbol` | Provider identity |
| Chart timezone | `source_timezone` | Correct timestamp interpretation |

Phase 1 will use the raw quote OHLC values and will not mix Yahoo adjusted-close values with Angel One prices. Corporate actions and adjusted-price policy will be handled in a later phase.

### 3.4 RBI DBIE: CPI inflation

#### Why this source

CPI is a monthly inflation indicator. It provides macroeconomic context for interpreting market movements and is a manageable first non-market time series.

#### Current source location

The older `dbieold.rbi.org.in` address may not work reliably because RBI changed the DBIE portal URL. Use the current RBI DBIE portal:

`https://dbie.rbihub.in/`

RBI publications identify `https://data.rbi.org.in/` as the newer DBIE data portal. [RBI publication describing the DBIE URL change](https://www.rbi.org.in/scripts/AnnualReportPublications.aspx?Id=1440)

#### How we will collect it

1. Open the current DBIE portal.
2. Locate the CPI time-series data.
3. Select the combined/all-India headline CPI series.
4. Export the monthly observations to CSV if the portal provides export.
5. Save the reviewed file under `data/seeds/`.
6. Record the exact series name, unit, base year, retrieval date, and portal URL in the seed README.
7. Load the CSV through the Go seed command.

If the portal export is not accessible, use an official RBI publication or official government CSV as a temporary seed and record the limitation rather than silently using an unverified third-party dataset.

**Dashboard verification (11 August 2026):** The RBI dashboard exposes `CPI (combined)` under `Inflation (%)`, with monthly rows. It showed 2.73 percent for January 2026. The dashboard did not expose a downloadable CSV in the reviewed view, so it is retained as the authoritative definition and validation source; a downloadable official seed will be selected during macro-seed implementation.

#### Values we will take

| Source value | Normalized value | Why we need it |
|---|---|---|
| Month or reference period | `observed_at` | Aligns inflation with market periods |
| CPI value or inflation rate | `value` | Core macroeconomic observation |
| Series name | `series_code` / `metric` | Identifies exactly which CPI measure was used |
| Unit | `unit` | Prevents confusion between index level and percent inflation |
| Frequency | `frequency` | Explains monthly sampling |
| Base year | `base_period` | Makes the index definition auditable |
| Source and retrieval date | provenance fields | Reproducibility and later updates |

Phase 1 will prefer headline CPI inflation as a percentage rate rather than collecting every CPI category.

### 3.5 RBI DBIE: policy repo rate

#### Why this source

The policy repo rate represents the RBI's policy-rate setting and is directly relevant to the macroeconomic context of Indian markets. Unlike CPI, it changes on policy decision dates rather than every trading day.

#### How we will collect it

Use the same DBIE portal workflow as CPI, selecting the policy repo-rate time series and exporting the observations to a reviewed CSV. Record whether each date represents the announcement date or effective date.

The DBIE portal currently displays policy repo rate and CPI inflation among its headline indicators. [Current RBI DBIE portal](https://dbie.rbihub.in/)

#### Values we will take

| Source value | Normalized value | Why we need it |
|---|---|---|
| Rate date | `observed_at` | Joins the rate with market and CPI timelines |
| Repo rate | `value` | Main monetary-policy value |
| Unit | `unit` = percent | Makes the numeric meaning explicit |
| Event/effective-date meaning | `observation_type` | Prevents incorrect time alignment |
| Series name | `series_code` / `metric` | Identifies the exact policy rate |
| Source and retrieval date | provenance fields | Reproducibility |

We will not interpolate the repo rate into daily rows in the warehouse. The dashboard can carry the latest known rate forward for visualization later, but the stored observations remain event-based.

**Dashboard verification (11 August 2026):** The `Policy repo` column is expressed as a percentage in monthly rows. It showed 5.25 percent for January and December 2025, 5.50 percent from June through November 2025, and 6.00 percent in April and May 2025. These are monthly snapshots, not proof of the original RBI policy-announcement date; the eventual seed must preserve the source's date meaning.

## 4. Common normalized values

All market providers must map their source values into the following common concepts:

| Normalized value | Applies to | Purpose |
|---|---|---|
| `instrument_id` | Market data | Internal stable database identity |
| `provider` | Market and macro data | Tells us where the record came from |
| `provider_symbol` | Market data | Preserves the source identifier |
| `provider_instrument_id` | Angel One and other providers | Preserves token or provider ID |
| `interval` | Market data | Distinguishes daily from future intervals |
| `observed_at` | All data | Common time-series key, stored in UTC |
| `open`, `high`, `low`, `close` | Market data | OHLC price fields |
| `volume` | Market data | Trading activity |
| `metric` | Macro data | Name of the economic measure |
| `value` | Macro data | Numeric macro observation |
| `unit` | Macro data | Percent, index points, or other unit |
| `source_retrieved_at` | All data | Provenance and reproducibility |

## 5. What will not be collected in Phase 1

- Live WebSocket ticks
- Order book depth
- Orders, holdings, or account balances
- Options and futures open interest
- Every available exchange instrument
- Every CPI subcategory
- Personal bank statements
- LLM-generated analysis

## 6. Collection checklist before database design

Phase 3.2 can proceed to schema design after the remaining non-Angel samples are reviewed. Angel One will be validated during adapter implementation using the live API and local credentials.

Checklist:

- [x] Open and verify each selected source or dashboard view.
- [~] Capture one sample Angel One response; deferred until the Angel One adapter is implemented.
- [x] Capture one Binance klines response.
- [x] Capture one Yahoo fallback response.
- [x] Verify the CPI dashboard series and its values; CSV export is unavailable in the reviewed view.
- [x] Verify the repo-rate dashboard series and its values; CSV export is unavailable in the reviewed view.
- [~] Select an official downloadable macro seed source during Phase 4.5.
- [x] Store available API samples as sanitized fixtures.
- [x] Record retrieval dates and source URLs in the research notes.
- [x] Confirm the selected fields and explanations above.

The first live Angel One response must still be checked for authentication, token resolution, timestamp format, candle shape, and provider-specific errors before the Angel adapter is considered complete.

The data-source research required for schema design is complete. We can finalize the PostgreSQL schema now, without waiting for the Angel One sample or downloadable macro seed files.
