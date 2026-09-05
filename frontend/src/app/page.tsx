import MarketDashboard from "@/components/market-dashboard";
import MacroDashboard from "@/components/macro-dashboard";
import { listInstruments, type Instrument } from "@/lib/api";

// Home is the server-rendered shell of the interactive dashboard.
//
// Its flow is:
//  1. Ask the API client for instruments.
//  2. Show a connection error if the Go API is unavailable.
//  3. Pass the catalog to the client-side market view for interaction.
//
// Because this function runs on the Next.js server, the API base URL remains a
// server-side setting and browser CORS is not involved in this first step.
export default async function Home() {
  let instruments: Instrument[] = [];
  let loadError: string | null = null;

  try {
    instruments = await listInstruments();
  } catch (error) {
    // Convert an unknown thrown value into a safe message for the page. The
    // detailed fetch error remains useful in the server terminal if needed,
    // while the user sees an actionable local-development instruction.
    loadError = error instanceof Error ? error.message : "Unable to connect to the MoneyPlant API";
  }

  return (
    <main className="min-h-screen px-6 py-10 sm:px-10 lg:px-16">
      <div className="mx-auto max-w-6xl">
        <header className="flex flex-col justify-between gap-6 border-b border-slate-200 pb-8 md:flex-row md:items-end">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.22em] text-growth">
              MoneyPlant
            </p>
            <h1 className="mt-3 text-4xl font-semibold tracking-tight text-ink">
              Market intelligence, grounded in data.
            </h1>
            <p className="mt-3 max-w-2xl text-base leading-7 text-slate-600">
              Explore market candles and macroeconomic indicators from the Go API and PostgreSQL data layer.
            </p>
          </div>
          <div className="rounded-xl border border-slate-200 bg-white px-4 py-3 text-sm text-slate-600 shadow-sm">
            <span className="font-medium text-slate-800">Phase 7.3</span> · Market and macro views
          </div>
        </header>

        {loadError ? (
          <section className="mt-10">
            <div className="mt-6 rounded-2xl border border-amber-200 bg-amber-50 p-6 text-amber-900">
              <h3 className="font-semibold">The Go API is not available</h3>
              <p className="mt-2 text-sm leading-6">
                Start the backend with <code className="rounded bg-amber-100 px-1 py-0.5">go run ./cmd/api</code>,
                then refresh this page.
              </p>
              <p className="mt-2 text-xs text-amber-800">Technical detail: {loadError}</p>
            </div>
          </section>
        ) : instruments.length === 0 ? (
          <div className="mt-10 rounded-2xl border border-slate-200 bg-white p-6 text-slate-600 shadow-sm">
            No active instruments are currently available.
          </div>
        ) : (
          <>
            <MarketDashboard instruments={instruments} />
            <MacroDashboard />
          </>
        )}
      </div>
    </main>
  );
}
