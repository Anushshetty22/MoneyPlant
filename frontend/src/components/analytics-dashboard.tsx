"use client";

import { useEffect, useMemo, useState } from "react";
import {
  listMarketAnalytics,
  listMarketComparison,
  type Instrument,
  type MarketAnalytics,
  type MarketAnalyticsPoint,
  type MarketComparison
} from "@/lib/api";

function activeSources(instrument: Instrument): Instrument["sources"] {
  return instrument.sources.filter((source) => source.is_active);
}

function authoritativeProvider(instrument: Instrument): string | null {
  return instrument.sources.find((source) => source.is_active && source.is_authoritative)?.provider
    ?? activeSources(instrument)[0]?.provider
    ?? null;
}

function formatPercent(value: string | null | undefined): string {
  if (value == null) {
    return "Not enough data";
  }
  const number = Number(value);
  return Number.isFinite(number) ? `${(number * 100).toFixed(2)}%` : "Unavailable";
}

function formatNumber(value: string | null | undefined): string {
  return value == null || value === "" ? "No data" : value;
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(value));
}

function MetricCard({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">{label}</p>
      <p className="mt-2 text-2xl font-semibold text-ink">{value}</p>
      <p className="mt-1 text-xs text-slate-500">{detail}</p>
    </div>
  );
}

type ChartSeries = {
  label: string;
  color: string;
  values: Array<number | null>;
};

function AnalyticsLineChart({
  title,
  description,
  labels,
  series,
  percentAxis = false
}: {
  title: string;
  description: string;
  labels: string[];
  series: ChartSeries[];
  percentAxis?: boolean;
}) {
  const width = 900;
  const height = 320;
  const padding = 48;
  const values = series.flatMap((item) => item.values).filter((value): value is number => value !== null && Number.isFinite(value));

  if (values.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-slate-300 p-6 text-sm text-slate-500">
        No chart values are available for this selection.
      </div>
    );
  }

  const minimum = Math.min(...values);
  const maximum = Math.max(...values);
  const range = maximum - minimum || 1;
  const usableWidth = width - padding * 2;
  const usableHeight = height - padding * 2;
  const xForIndex = (index: number) => padding + (index / Math.max(labels.length - 1, 1)) * usableWidth;
  const yForValue = (value: number) => height - padding - ((value - minimum) / range) * usableHeight;

  const renderSegments = (item: ChartSeries) => {
    const segments: string[][] = [];
    let current: string[] = [];
    item.values.forEach((value, index) => {
      if (value == null || !Number.isFinite(value)) {
        if (current.length > 0) {
          segments.push(current);
          current = [];
        }
        return;
      }
      current.push(`${xForIndex(index)},${yForValue(value)}`);
    });
    if (current.length > 0) {
      segments.push(current);
    }
    return segments.map((points, index) => (
      <polyline
        key={`${item.label}-${index}`}
        fill="none"
        stroke={item.color}
        strokeWidth="3"
        points={points.join(" ")}
      />
    ));
  };

  return (
    <div>
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-baseline sm:justify-between">
        <div>
          <h3 className="text-lg font-semibold text-ink">{title}</h3>
          <p className="text-sm text-slate-500">{description}</p>
        </div>
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-600">
          {series.map((item) => (
            <span key={item.label} className="inline-flex items-center gap-1.5">
              <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: item.color }} />
              {item.label}
            </span>
          ))}
        </div>
      </div>
      <svg viewBox={`0 0 ${width} ${height}`} className="h-auto w-full" role="img" aria-label={title}>
        {[0, 0.5, 1].map((fraction) => {
          const y = padding + fraction * usableHeight;
          const value = maximum - fraction * range;
          return (
            <g key={fraction}>
              <line x1={padding} x2={width - padding} y1={y} y2={y} stroke="#e2e8f0" strokeDasharray="4 4" />
              <text x={8} y={y + 4} fontSize="12" fill="#64748b">
                {percentAxis ? `${value.toFixed(1)}%` : value.toFixed(2)}
              </text>
            </g>
          );
        })}
        {series.flatMap(renderSegments)}
        {labels.length > 0 && (
          <>
            <text x={padding} y={height - 12} fontSize="12" fill="#64748b">
              {labels[0]?.slice(0, 10)}
            </text>
            <text x={width - padding} y={height - 12} textAnchor="end" fontSize="12" fill="#64748b">
              {labels[labels.length - 1]?.slice(0, 10)}
            </text>
          </>
        )}
      </svg>
    </div>
  );
}

function analyticsPriceSeries(points: MarketAnalyticsPoint[]): ChartSeries[] {
  return [
    { label: "Close", color: "#0f766e", values: points.map((point) => Number(point.close)) },
    { label: "SMA 7", color: "#2563eb", values: points.map((point) => point.sma_7 == null ? null : Number(point.sma_7)) },
    { label: "SMA 20", color: "#7c3aed", values: points.map((point) => point.sma_20 == null ? null : Number(point.sma_20)) },
    { label: "SMA 50", color: "#ea580c", values: points.map((point) => point.sma_50 == null ? null : Number(point.sma_50)) }
  ];
}

function EmptyAnalyticsState({ message }: { message: string }) {
  return (
    <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-8 text-slate-600 shadow-sm">
      {message}
    </div>
  );
}

export default function AnalyticsDashboard({ instruments }: { instruments: Instrument[] }) {
  const [selectedSymbol, setSelectedSymbol] = useState(instruments[0]?.canonical_symbol ?? "");
  const [selectedProvider, setSelectedProvider] = useState(
    instruments[0] ? authoritativeProvider(instruments[0]) ?? "" : ""
  );
  const [from, setFrom] = useState("2026-08-01");
  const [to, setTo] = useState("2026-09-01");
  const [comparisonSymbols, setComparisonSymbols] = useState<string[]>([]);
  const [analytics, setAnalytics] = useState<MarketAnalytics | null>(null);
  const [comparison, setComparison] = useState<MarketComparison | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const selectedInstrument = useMemo(
    () => instruments.find((instrument) => instrument.canonical_symbol === selectedSymbol),
    [instruments, selectedSymbol]
  );

  const availableSources = useMemo(
    () => selectedInstrument ? activeSources(selectedInstrument) : [],
    [selectedInstrument]
  );

  const comparableInstruments = useMemo(
    () => instruments.filter((instrument) => instrument.canonical_symbol !== selectedSymbol
      && activeSources(instrument).some((source) => source.provider === selectedProvider)),
    [instruments, selectedProvider, selectedSymbol]
  );

  useEffect(() => {
    if (!selectedInstrument) {
      setSelectedProvider("");
      return;
    }
    setSelectedProvider((currentProvider) =>
      availableSources.some((source) => source.provider === currentProvider)
        ? currentProvider
        : authoritativeProvider(selectedInstrument) ?? ""
    );
    setComparisonSymbols([]);
  }, [availableSources, selectedInstrument]);

  useEffect(() => {
    setComparisonSymbols((currentSymbols) => currentSymbols.filter((symbol) =>
      comparableInstruments.some((instrument) => instrument.canonical_symbol === symbol)
    ));
  }, [comparableInstruments]);

  useEffect(() => {
    if (!selectedSymbol || !selectedProvider || !from || !to) {
      setAnalytics(null);
      setComparison(null);
      return;
    }

    const controller = new AbortController();
    setIsLoading(true);
    setError(null);
    const symbols = [selectedSymbol, ...comparisonSymbols];

    Promise.all([
      listMarketAnalytics(selectedSymbol, selectedProvider, from, to, controller.signal),
      symbols.length > 1
        ? listMarketComparison(symbols, selectedProvider, from, to, controller.signal)
        : Promise.resolve(null)
    ])
      .then(([loadedAnalytics, loadedComparison]) => {
        setAnalytics(loadedAnalytics);
        setComparison(loadedComparison);
      })
      .catch((requestError: unknown) => {
        if (requestError instanceof DOMException && requestError.name === "AbortError") {
          return;
        }
        setAnalytics(null);
        setComparison(null);
        setError(requestError instanceof Error ? requestError.message : "Unable to load market analytics");
      })
      .finally(() => setIsLoading(false));

    return () => controller.abort();
  }, [comparisonSymbols, from, selectedProvider, selectedSymbol, to]);

  const summary = analytics?.summary;
  const points = analytics?.series ?? [];
  const labels = points.map((point) => point.observed_at);
  const comparisonSeries = comparison?.series ?? [];

  return (
    <section id="analytics-view" className="mt-10 scroll-mt-20">
      <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
        <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.18em] text-growth">Analytics</p>
            <h2 className="mt-2 text-2xl font-semibold text-ink">Market performance</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-500">
              Explore daily returns, moving averages, volatility, drawdown, and normalized performance.
            </p>
          </div>

          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <label className="text-sm font-medium text-slate-700">
              Instrument
              <select value={selectedSymbol} onChange={(event) => setSelectedSymbol(event.target.value)} className="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-normal text-slate-800">
                {instruments.map((instrument) => (
                  <option key={instrument.id} value={instrument.canonical_symbol}>{instrument.canonical_symbol}</option>
                ))}
              </select>
            </label>
            <label className="text-sm font-medium text-slate-700">
              Source
              <select value={selectedProvider} onChange={(event) => setSelectedProvider(event.target.value)} className="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-normal text-slate-800" disabled={availableSources.length === 0}>
                {availableSources.map((source) => (
                  <option key={source.provider} value={source.provider}>{source.provider}</option>
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

        <div className="mt-5 flex flex-col gap-4 border-t border-slate-100 pt-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="text-xs text-slate-500">
            <span>Interval: <strong className="font-medium text-slate-700">1d</strong></span>
            <span className="ml-5">Times are UTC</span>
            <p className="mt-2">Moving averages use candle observations, not calendar days.</p>
          </div>
          <label className="text-sm font-medium text-slate-700 lg:w-80">
            Compare with
            <select
              multiple
              size={Math.min(Math.max(comparableInstruments.length, 2), 5)}
              value={comparisonSymbols}
              onChange={(event) => setComparisonSymbols(Array.from(event.target.selectedOptions, (option) => option.value))}
              className="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-normal text-slate-800"
              disabled={comparableInstruments.length === 0}
            >
              {comparableInstruments.map((instrument) => (
                <option key={instrument.id} value={instrument.canonical_symbol}>{instrument.canonical_symbol}</option>
              ))}
            </select>
            <span className="mt-1 block text-xs font-normal text-slate-500">Use Command/Ctrl-click to select multiple instruments.</span>
          </label>
        </div>
      </div>

      {isLoading ? (
        <EmptyAnalyticsState message="Loading market analytics…" />
      ) : error ? (
        <div className="mt-6 rounded-2xl border border-amber-200 bg-amber-50 p-6 text-amber-900">
          <h3 className="font-semibold">Unable to load analytics</h3>
          <p className="mt-2 text-sm">Check that the Go API is running and that the selected date range is valid.</p>
          <p className="mt-2 text-xs">Technical detail: {error}</p>
        </div>
      ) : !selectedProvider ? (
        <EmptyAnalyticsState message="No active provider source is configured for this instrument." />
      ) : !summary || points.length === 0 ? (
        <EmptyAnalyticsState message="No daily candles were found for this selection." />
      ) : (
        <>
          <div className="mt-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <MetricCard label="Latest close" value={formatNumber(summary.last_close)} detail={`${summary.candle_count} daily candles`} />
            <MetricCard label="Total return" value={formatPercent(summary.total_return)} detail={`${formatDate(summary.first_observed_at)} to ${formatDate(summary.last_observed_at)}`} />
            <MetricCard label="Maximum drawdown" value={formatPercent(summary.maximum_drawdown)} detail="Largest peak-to-trough decline" />
            <MetricCard label="20-day volatility" value={formatPercent(summary.annualized_volatility_20)} detail="Annualized from daily returns" />
          </div>

          <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
            <AnalyticsLineChart
              title="Price and moving averages"
              description="Moving averages become visible after their observation windows warm up."
              labels={labels}
              series={analyticsPriceSeries(points)}
            />
            <div className="mt-10 border-t border-slate-100 pt-8">
              <AnalyticsLineChart
                title="Cumulative performance"
                description="The selected range starts at 100; the values show how the investment would have changed proportionally."
                labels={labels}
                series={[{
                  label: analytics.canonical_symbol,
                  color: "#0f766e",
                  values: points.map((point) => point.cumulative_return == null ? null : 100 * (1 + Number(point.cumulative_return)))
                }]}
              />
            </div>
            <div className="mt-10 border-t border-slate-100 pt-8">
              <AnalyticsLineChart
                title="Drawdown"
                description="Distance below the highest close reached within the selected range."
                labels={labels}
                percentAxis
                series={[{
                  label: "Drawdown",
                  color: "#dc2626",
                  values: points.map((point) => point.drawdown == null ? null : 100 * Number(point.drawdown))
                }]}
              />
            </div>
          </div>

          {comparisonSeries.length > 1 && (
            <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
              <AnalyticsLineChart
                title="Normalized performance comparison"
                description="Every selected instrument starts at 100 so relative performance is easy to compare."
                labels={comparisonSeries[0]?.series.map((point) => point.observed_at) ?? []}
                series={comparisonSeries.map((item, index) => ({
                  label: item.canonical_symbol,
                  color: ["#0f766e", "#2563eb", "#7c3aed", "#ea580c", "#db2777", "#0891b2"][index % 6],
                  values: item.series.map((point) => Number(point.normalized_close))
                }))}
              />
            </div>
          )}
        </>
      )}
    </section>
  );
}
