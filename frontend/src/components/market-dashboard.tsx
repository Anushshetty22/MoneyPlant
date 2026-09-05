"use client";

import { useEffect, useMemo, useState } from "react";
import { listCandles, type Candle, type Instrument } from "@/lib/api";

// The Phase 1 source decision currently maps crypto to Binance and equities to
// Yahoo Finance. This small resolver keeps that initial rule visible; a future
// instrument-source API can replace it when more providers are added.
function providerForInstrument(instrument: Instrument): string {
  return instrument.asset_type === "crypto" ? "binance" : "yahoo";
}

// PriceChart turns close-price strings into SVG coordinates. SVG is used here
// intentionally so the learner can see the chart fundamentals instead of only
// configuring a third-party chart library.
function PriceChart({ candles }: { candles: Candle[] }) {
  const width = 900;
  const height = 340;
  const padding = 48;
  const values = candles.map((candle) => Number(candle.close));
  const minimum = Math.min(...values);
  const maximum = Math.max(...values);
  const range = maximum - minimum || 1;
  const usableWidth = width - padding * 2;
  const usableHeight = height - padding * 2;

  const points = candles
    .map((candle, index) => {
      const x = padding + (index / Math.max(candles.length - 1, 1)) * usableWidth;
      const y = height - padding - ((Number(candle.close) - minimum) / range) * usableHeight;
      return `${x},${y}`;
    })
    .join(" ");

  return (
    <div>
      <div className="mb-3 flex items-baseline justify-between">
        <div>
          <h3 className="text-lg font-semibold text-ink">Closing price</h3>
          <p className="text-sm text-slate-500">Each point represents one {candles[0]?.interval} candle.</p>
        </div>
        <p className="text-sm text-slate-500">
          {minimum.toFixed(2)} – {maximum.toFixed(2)}
        </p>
      </div>

      <svg viewBox={`0 0 ${width} ${height}`} className="h-auto w-full" role="img" aria-label="Closing price chart">
        {[0, 0.5, 1].map((fraction) => {
          const y = padding + fraction * usableHeight;
          const value = maximum - fraction * range;
          return (
            <g key={fraction}>
              <line x1={padding} x2={width - padding} y1={y} y2={y} stroke="#e2e8f0" strokeDasharray="4 4" />
              <text x={8} y={y + 4} fontSize="12" fill="#64748b">
                {value.toFixed(2)}
              </text>
            </g>
          );
        })}
        <polyline fill="none" stroke="#0f766e" strokeWidth="3" points={points} />
        {candles.map((candle, index) => {
          const x = padding + (index / Math.max(candles.length - 1, 1)) * usableWidth;
          const y = height - padding - ((Number(candle.close) - minimum) / range) * usableHeight;
          return <circle key={candle.id} cx={x} cy={y} r="4" fill="#0f766e" />;
        })}
        <text x={padding} y={height - 12} fontSize="12" fill="#64748b">
          {candles[0]?.observed_at.slice(0, 10)}
        </text>
        <text x={width - padding} y={height - 12} textAnchor="end" fontSize="12" fill="#64748b">
          {candles[candles.length - 1]?.observed_at.slice(0, 10)}
        </text>
      </svg>
    </div>
  );
}

// VolumeChart uses the same x-axis positions but maps volume to bar height.
// Keeping it as a separate chart makes price movement and trading activity
// visually comparable without mixing different units on one y-axis.
function VolumeChart({ candles }: { candles: Candle[] }) {
  const width = 900;
  const height = 180;
  const padding = 48;
  const maximumVolume = Math.max(...candles.map((candle) => Number(candle.volume)), 1);
  const usableWidth = width - padding * 2;
  const usableHeight = height - padding * 2;
  const barWidth = Math.max(3, (usableWidth / Math.max(candles.length, 1)) * 0.65);

  return (
    <div className="mt-8">
      <div className="mb-3">
        <h3 className="text-lg font-semibold text-ink">Volume</h3>
        <p className="text-sm text-slate-500">The amount traded during each candle.</p>
      </div>
      <svg viewBox={`0 0 ${width} ${height}`} className="h-auto w-full" role="img" aria-label="Volume chart">
        <line x1={padding} x2={width - padding} y1={height - padding} y2={height - padding} stroke="#cbd5e1" />
        {candles.map((candle, index) => {
          const volume = Number(candle.volume);
          const barHeight = (volume / maximumVolume) * usableHeight;
          const x = padding + (index / Math.max(candles.length - 1, 1)) * usableWidth - barWidth / 2;
          const y = height - padding - barHeight;
          return <rect key={candle.id} x={x} y={y} width={barWidth} height={barHeight} rx="2" fill="#99f6e4" />;
        })}
      </svg>
    </div>
  );
}

// MarketDashboard owns browser interaction and data loading for the first chart
// view. The page supplies the initial instrument catalog from the server.
export default function MarketDashboard({ instruments }: { instruments: Instrument[] }) {
  const [selectedSymbol, setSelectedSymbol] = useState(instruments[0]?.canonical_symbol ?? "");
  const [from, setFrom] = useState("2026-08-01");
  const [to, setTo] = useState("2026-08-07");
  const [candles, setCandles] = useState<Candle[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const selectedInstrument = useMemo(
    () => instruments.find((instrument) => instrument.canonical_symbol === selectedSymbol),
    [instruments, selectedSymbol]
  );

  useEffect(() => {
    if (!selectedInstrument || !from || !to) {
      return;
    }

    const controller = new AbortController();
    setIsLoading(true);
    setError(null);

    // Fetching inside the effect means the chart refreshes when the user changes
    // the instrument or date window. Abort prevents an older request from
    // updating the chart after a newer selection has already been made.
    listCandles(
      selectedInstrument.canonical_symbol,
      providerForInstrument(selectedInstrument),
      "1d",
      from,
      to,
      controller.signal
    )
      .then(setCandles)
      .catch((requestError: unknown) => {
        if (requestError instanceof DOMException && requestError.name === "AbortError") {
          return;
        }
        setCandles([]);
        setError(requestError instanceof Error ? requestError.message : "Unable to load candles");
      })
      .finally(() => setIsLoading(false));

    return () => controller.abort();
  }, [from, selectedInstrument, to]);

  return (
    <section className="mt-10">
      <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
        <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.18em] text-growth">Market view</p>
            <h2 className="mt-2 text-2xl font-semibold text-ink">Daily market candles</h2>
            <p className="mt-1 text-sm text-slate-500">Choose an instrument and a UTC date window.</p>
          </div>

          <div className="grid gap-4 sm:grid-cols-3">
            <label className="text-sm font-medium text-slate-700">
              Instrument
              <select
                value={selectedSymbol}
                onChange={(event) => setSelectedSymbol(event.target.value)}
                className="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-normal text-slate-800"
              >
                {instruments.map((instrument) => (
                  <option key={instrument.id} value={instrument.canonical_symbol}>
                    {instrument.canonical_symbol}
                  </option>
                ))}
              </select>
            </label>
            <label className="text-sm font-medium text-slate-700">
              From
              <input type="date" value={from} onChange={(event) => setFrom(event.target.value)} className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 font-normal text-slate-800" />
            </label>
            <label className="text-sm font-medium text-slate-700">
              To (exclusive)
              <input type="date" value={to} onChange={(event) => setTo(event.target.value)} className="mt-1 block w-full rounded-lg border border-slate-300 px-3 py-2 font-normal text-slate-800" />
            </label>
          </div>
        </div>

        {selectedInstrument && (
          <p className="mt-5 text-xs text-slate-500">
            Source: <span className="font-medium text-slate-700">{providerForInstrument(selectedInstrument)}</span> · Interval: <span className="font-medium text-slate-700">1d</span> · Times are UTC
          </p>
        )}
      </div>

      {isLoading ? (
        <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-8 text-slate-600 shadow-sm">Loading candles…</div>
      ) : error ? (
        <div className="mt-6 rounded-2xl border border-amber-200 bg-amber-50 p-6 text-amber-900">
          <h3 className="font-semibold">Unable to load market data</h3>
          <p className="mt-2 text-sm">Check that the Go API is running and that the selected date range contains stored candles.</p>
          <p className="mt-2 text-xs">Technical detail: {error}</p>
        </div>
      ) : candles.length === 0 ? (
        <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-8 text-slate-600 shadow-sm">No candles were found for this selection.</div>
      ) : (
        <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
          <PriceChart candles={candles} />
          <VolumeChart candles={candles} />
        </div>
      )}
    </section>
  );
}

