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

type DataResponse<T> = {
  data: T[];
};

// The environment variable is read on the Next.js server for this first
// server-rendered page. The fallback makes local development work even before
// the developer copies .env.example into .env.local.
const apiBaseURL = process.env.MONEYPLANT_API_BASE_URL ?? "http://localhost:8080";

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
