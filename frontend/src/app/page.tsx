import { listInstruments, type Instrument } from "@/lib/api";

// InstrumentCard renders one API instrument in a consistent visual block.
// It is deliberately a server component for Phase 7.1; interactive selection
// will be introduced after the API client and page foundation are verified.
function InstrumentCard({ instrument }: { instrument: Instrument }) {
  return (
    <article className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium uppercase tracking-[0.18em] text-slate-500">
            {instrument.asset_type}
          </p>
          <h2 className="mt-2 text-xl font-semibold text-ink">
            {instrument.canonical_symbol}
          </h2>
          <p className="mt-1 text-sm text-slate-600">{instrument.name}</p>
        </div>
        <span className="rounded-full bg-teal-50 px-3 py-1 text-xs font-semibold text-growth">
          Active
        </span>
      </div>

      <dl className="mt-6 grid grid-cols-2 gap-4 border-t border-slate-100 pt-4 text-sm">
        <div>
          <dt className="text-slate-500">Exchange</dt>
          <dd className="mt-1 font-medium text-slate-800">{instrument.exchange ?? "—"}</dd>
        </div>
        <div>
          <dt className="text-slate-500">Currency</dt>
          <dd className="mt-1 font-medium text-slate-800">{instrument.currency}</dd>
        </div>
      </dl>
    </article>
  );
}

// Home is the first server-rendered dashboard page.
//
// Its flow is:
//  1. Ask the API client for instruments.
//  2. Show a connection error if the Go API is unavailable.
//  3. Render a card for every instrument returned by PostgreSQL.
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
              Your first read-only dashboard view is connected to the Go API and
              PostgreSQL data layer.
            </p>
          </div>
          <div className="rounded-xl border border-slate-200 bg-white px-4 py-3 text-sm text-slate-600 shadow-sm">
            <span className="font-medium text-slate-800">Phase 7.1</span> · Frontend foundation
          </div>
        </header>

        <section className="mt-10">
          <div className="flex items-end justify-between gap-4">
            <div>
              <h2 className="text-2xl font-semibold text-ink">Available instruments</h2>
              <p className="mt-1 text-sm text-slate-500">
                Loaded from the MoneyPlant instrument catalog.
              </p>
            </div>
            {!loadError && (
              <p className="text-sm font-medium text-slate-500">
                {instruments.length} instrument{instruments.length === 1 ? "" : "s"}
              </p>
            )}
          </div>

          {loadError ? (
            <div className="mt-6 rounded-2xl border border-amber-200 bg-amber-50 p-6 text-amber-900">
              <h3 className="font-semibold">The Go API is not available</h3>
              <p className="mt-2 text-sm leading-6">
                Start the backend with <code className="rounded bg-amber-100 px-1 py-0.5">go run ./cmd/api</code>,
                then refresh this page.
              </p>
              <p className="mt-2 text-xs text-amber-800">Technical detail: {loadError}</p>
            </div>
          ) : instruments.length === 0 ? (
            <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-6 text-slate-600 shadow-sm">
              No active instruments are currently available.
            </div>
          ) : (
            <div className="mt-6 grid gap-5 md:grid-cols-2">
              {instruments.map((instrument) => (
                <InstrumentCard key={instrument.id} instrument={instrument} />
              ))}
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
