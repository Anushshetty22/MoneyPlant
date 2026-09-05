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

type DataResponse<T> = {
  data: T[];
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
