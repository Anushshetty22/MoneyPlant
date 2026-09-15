// The API client is the single place where the frontend knows the Go API URL
// and response envelope. Components call named functions instead of building
// URLs and parsing JSON independently.

// Instrument is the frontend representation of one canonical MoneyPlant asset.
// The property names match the JSON contract defined by the Go API.
export type Instrument = {
  id: number;
  canonical_symbol: string;
  name: string;
  asset_type: string;
  exchange: string | null;
  currency: string;
  is_active: boolean;
  sources: InstrumentSource[];
};

// InstrumentSource describes how one canonical MoneyPlant symbol is
// represented by a provider. The frontend uses the authoritative active source
// returned by the API instead of hardcoding provider-specific symbols or tokens.
export type InstrumentSource = {
  provider: string;
  provider_symbol: string;
  provider_instrument_id: string | null;
  is_authoritative: boolean;
  is_active: boolean;
};

// Candle is the frontend representation of one market OHLCV observation.
// Financial decimals remain strings because the Go API intentionally preserves
// PostgreSQL NUMERIC precision at the HTTP boundary.
export type Candle = {
  id: number;
  interval: string;
  observed_at: string;
  source_close_at: string | null;
  open: string;
  high: string;
  low: string;
  close: string;
  volume: string;
  quote_volume: string | null;
  trade_count: number | null;
  taker_buy_volume: string | null;
  taker_buy_quote_volume: string | null;
  source_retrieved_at: string;
};

// MarketAnalytics contains on-demand daily calculations from the Phase 4
// analytics API. Calculated decimals remain strings; nullable rolling metrics
// indicate that their warm-up window is not available yet.
export type MarketAnalytics = {
  canonical_symbol: string;
  provider: string;
  interval: string;
  from: string;
  to: string;
  summary: MarketAnalyticsSummary | null;
  series: MarketAnalyticsPoint[];
};

export type MarketAnalyticsSummary = {
  candle_count: number;
  first_observed_at: string;
  last_observed_at: string;
  first_close: string;
  last_close: string;
  total_return: string | null;
  maximum_drawdown: string | null;
  annualized_volatility_20: string | null;
};

export type MarketAnalyticsPoint = {
  observed_at: string;
  close: string;
  period_return: string | null;
  cumulative_return: string | null;
  sma_7: string | null;
  sma_20: string | null;
  sma_50: string | null;
  volatility_20: string | null;
  drawdown: string | null;
};

export type MarketComparison = {
  provider: string;
  interval: string;
  from: string;
  to: string;
  series: MarketComparisonSeries[];
};

export type MarketComparisonSeries = {
  canonical_symbol: string;
  first_close: string;
  last_close: string;
  total_return: string | null;
  series: MarketComparisonPoint[];
};

export type MarketComparisonPoint = {
  observed_at: string;
  normalized_close: string;
};

// MacroDataset describes the meaning and provenance of one macroeconomic
// series. The dashboard uses it to label values correctly instead of displaying
// an unexplained number.
export type MacroDataset = {
  id: number;
  code: string;
  name: string;
  provider: string;
  metric: string;
  unit: string;
  frequency: string;
  observation_type: string;
  base_period: string | null;
  source_url: string;
  retrieved_at: string;
  is_active: boolean;
};

// MacroObservation contains one date and exact decimal value from a macro
// dataset. Values remain strings until the chart maps them to SVG coordinates.
export type MacroObservation = {
  id: number;
  observed_on: string;
  value: string;
  source_retrieved_at: string;
  source_row_reference: string | null;
};

// LiveSnapshot represents the latest in-memory trade returned by the Phase 2
// live-monitor endpoint. Price and quantity remain strings so the browser does
// not accidentally round exact decimal values through JavaScript numbers.
export type LiveSnapshot = {
	canonical_symbol: string;
	provider: string;
	provider_symbol: string;
  event_type: string;
  observed_at: string;
  price: string;
  quantity: string;
  source_received_at: string;
};

// LiveMonitorStatus describes the backend monitor itself rather than a market
// value. The dashboard uses it to display connection state and event counts.
export type LiveMonitorStatus = {
  enabled: boolean;
  provider: string;
  canonical_symbol: string;
  provider_symbol: string;
  state: "disabled" | "starting" | "running" | "reconnecting" | "stopped" | "error";
  received: number;
  accepted: number;
  rejected: number;
  reconnects: number;
  persisted: number;
  restored: number;
  last_event_observed_at: string | null;
  last_event_source_received_at: string | null;
  last_persisted_at: string | null;
  last_error: string | null;
  last_reconnect_error: string | null;
  last_persistence_error: string | null;
  updated_at: string;
};

type DataResponse<T> = {
  data: T[];
};

type SingleDataResponse<T> = {
  data: T;
};

// The environment variable is read on the Next.js server for this first
// server-rendered page. The fallback makes local development work even before
// the developer copies .env.example into .env.local.
const apiBaseURL = process.env.MONEYPLANT_API_BASE_URL ?? "http://localhost:8080";

// Browser components call the same-origin Next.js proxy. The proxy then calls
// the Go API server-side, so the browser does not need direct cross-origin
// access to port 8080.
const browserAPIBaseURL = "/api/backend";

// listInstruments requests active instruments from the Go REST API.
//
// The fetch is marked no-store because MoneyPlant is a data application: during
// development, a newly ingested instrument should be visible on the next page
// request rather than being served from a stale framework cache.
export async function listInstruments(): Promise<Instrument[]> {
  const response = await fetch(`${apiBaseURL}/api/v1/instruments`, {
    cache: "no-store"
  });

  // A non-2xx response is converted into a normal Error so the page can show a
  // useful connection message instead of trying to parse an error as data.
  if (!response.ok) {
    throw new Error(`MoneyPlant API returned HTTP ${response.status}`);
  }

  const payload = (await response.json()) as DataResponse<Instrument>;
  return payload.data;
}

// listCandles retrieves candles for the interactive market view.
//
// The API uses an exclusive `to` timestamp. The page therefore sends the date
// inputs as midnight UTC values and the Go API applies the half-open range.
export async function listCandles(
  symbol: string,
  provider: string,
  interval: string,
  from: string,
  to: string,
  signal?: AbortSignal
): Promise<Candle[]> {
  const query = new URLSearchParams({
    symbol,
    provider,
    interval,
    from: `${from}T00:00:00Z`,
    to: `${to}T00:00:00Z`
  });

  const response = await fetch(
    `${browserAPIBaseURL}/api/v1/candles?${query.toString()}`,
    {
      cache: "no-store",
      signal
    }
  );

  if (!response.ok) {
    throw new Error(`MoneyPlant API returned HTTP ${response.status}`);
  }

  const payload = (await response.json()) as DataResponse<Candle>;
  return payload.data;
}

// listMarketAnalytics retrieves the calculated daily analytics for one
// canonical instrument. The API performs the indicator warm-up lookback.
export async function listMarketAnalytics(
  symbol: string,
  provider: string,
  from: string,
  to: string,
  signal?: AbortSignal
): Promise<MarketAnalytics> {
  const query = new URLSearchParams({
    symbol,
    provider,
    interval: "1d",
    from: `${from}T00:00:00Z`,
    to: `${to}T00:00:00Z`
  });
  const response = await fetch(`${browserAPIBaseURL}/api/v1/analytics/market?${query.toString()}`, {
    cache: "no-store",
    signal
  });
  if (!response.ok) {
    throw new Error(`MoneyPlant API returned HTTP ${response.status}`);
  }
  const payload = (await response.json()) as SingleDataResponse<MarketAnalytics>;
  return payload.data;
}

// listMarketComparison retrieves normalized performance for a selected group
// of instruments that share one provider and daily interval.
export async function listMarketComparison(
  symbols: string[],
  provider: string,
  from: string,
  to: string,
  signal?: AbortSignal
): Promise<MarketComparison> {
  const query = new URLSearchParams({
    symbols: symbols.join(","),
    provider,
    interval: "1d",
    from: `${from}T00:00:00Z`,
    to: `${to}T00:00:00Z`
  });
  const response = await fetch(`${browserAPIBaseURL}/api/v1/analytics/compare?${query.toString()}`, {
    cache: "no-store",
    signal
  });
  if (!response.ok) {
    throw new Error(`MoneyPlant API returned HTTP ${response.status}`);
  }
  const payload = (await response.json()) as SingleDataResponse<MarketComparison>;
  return payload.data;
}

// listMacroDatasets loads the active series definitions used by the macro
// selector. Browser requests go through the same-origin Next.js proxy.
export async function listMacroDatasets(signal?: AbortSignal): Promise<MacroDataset[]> {
  const response = await fetch(`${browserAPIBaseURL}/api/v1/macro/datasets`, {
    cache: "no-store",
    signal
  });

  if (!response.ok) {
    throw new Error(`MoneyPlant API returned HTTP ${response.status}`);
  }

  const payload = (await response.json()) as DataResponse<MacroDataset>;
  return payload.data;
}

// listMacroObservations loads one macro series, optionally restricted to a
// date-only half-open range. Macro dates intentionally do not include a time.
export async function listMacroObservations(
  dataset: string,
  from?: string,
  to?: string,
  signal?: AbortSignal
): Promise<MacroObservation[]> {
  const query = new URLSearchParams({ dataset });
  if (from && to) {
    query.set("from", from);
    query.set("to", to);
  }

  const response = await fetch(
    `${browserAPIBaseURL}/api/v1/macro/observations?${query.toString()}`,
    {
      cache: "no-store",
      signal
    }
  );

  if (!response.ok) {
    throw new Error(`MoneyPlant API returned HTTP ${response.status}`);
  }

  const payload = (await response.json()) as DataResponse<MacroObservation>;
  return payload.data;
}

// listLiveSnapshots loads the latest event for one provider symbol. The Go API
// returns an array because it also supports listing every monitored symbol;
// this client helper keeps the dashboard focused on the selected instrument.
export async function listLiveSnapshots(
  symbol: string,
  provider?: string,
  signal?: AbortSignal
): Promise<LiveSnapshot | null> {
  const query = new URLSearchParams({ symbol });
  if (provider) {
    query.set("provider", provider);
  }
  const response = await fetch(`${browserAPIBaseURL}/api/v1/live/snapshots?${query.toString()}`, {
    cache: "no-store",
    signal
  });

  if (!response.ok) {
    throw new Error(`MoneyPlant API returned HTTP ${response.status}`);
  }

  const payload = (await response.json()) as DataResponse<LiveSnapshot>;
  return payload.data[0] ?? null;
}

// liveStreamURL builds the same-origin SSE URL used by the dashboard. Keeping
// the proxy path here prevents components from knowing API routing details.
export function liveStreamURL(symbol: string, provider?: string): string {
  const query = new URLSearchParams({ symbol });
  if (provider) {
    query.set("provider", provider);
  }
  return `${browserAPIBaseURL}/api/v1/live/stream?${query.toString()}`;
}

// getLiveMonitorStatus loads operational metadata for the selected backend
// monitor. It is separate from listLiveSnapshots because a monitor can be
// running before its first event arrives, or stopped after its last snapshot.
export async function getLiveMonitorStatus(
  symbol?: string,
  provider?: string,
  signal?: AbortSignal
): Promise<LiveMonitorStatus> {
  const query = new URLSearchParams();
  if (symbol) {
    query.set("symbol", symbol);
  }
  if (provider) {
    query.set("provider", provider);
  }
  const queryString = query.toString();
  const response = await fetch(`${browserAPIBaseURL}/api/v1/live/status${queryString ? `?${queryString}` : ""}`, {
    cache: "no-store",
    signal
  });

  if (!response.ok) {
    throw new Error(`MoneyPlant API returned HTTP ${response.status}`);
  }

  const payload = (await response.json()) as { data: LiveMonitorStatus };
  return payload.data;
}
