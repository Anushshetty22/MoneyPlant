"use client";

import { useEffect, useMemo, useState } from "react";
import {
  listMacroDatasets,
  listMacroObservations,
  type MacroDataset,
  type MacroObservation
} from "@/lib/api";

// MacroLineChart maps date/value observations to SVG coordinates. The chart is
// intentionally similar to the market chart so the learner can compare the
// visualization pattern while seeing that macro data uses dates rather than
// intraday timestamps.
function MacroLineChart({ observations, unit }: { observations: MacroObservation[]; unit: string }) {
  const width = 900;
  const height = 320;
  const padding = 48;
  const values = observations.map((observation) => Number(observation.value));
  const minimum = Math.min(...values);
  const maximum = Math.max(...values);
  const range = maximum - minimum || 1;
  const usableWidth = width - padding * 2;
  const usableHeight = height - padding * 2;

  const points = observations
    .map((observation, index) => {
      const x = padding + (index / Math.max(observations.length - 1, 1)) * usableWidth;
      const y = height - padding - ((Number(observation.value) - minimum) / range) * usableHeight;
      return `${x},${y}`;
    })
    .join(" ");

  return (
    <div>
      <div className="mb-3 flex items-baseline justify-between">
        <div>
          <h3 className="text-lg font-semibold text-ink">Time series</h3>
          <p className="text-sm text-slate-500">Values shown in {unit}.</p>
        </div>
        <p className="text-sm text-slate-500">
          {minimum.toFixed(2)} – {maximum.toFixed(2)}
        </p>
      </div>

      <svg viewBox={`0 0 ${width} ${height}`} className="h-auto w-full" role="img" aria-label="Macroeconomic time series chart">
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
        <polyline fill="none" stroke="#7c3aed" strokeWidth="3" points={points} />
        {observations.map((observation, index) => {
          const x = padding + (index / Math.max(observations.length - 1, 1)) * usableWidth;
          const y = height - padding - ((Number(observation.value) - minimum) / range) * usableHeight;
          return <circle key={observation.id} cx={x} cy={y} r="4" fill="#7c3aed" />;
        })}
        <text x={padding} y={height - 12} fontSize="12" fill="#64748b">
          {observations[0]?.observed_on}
        </text>
        <text x={width - padding} y={height - 12} textAnchor="end" fontSize="12" fill="#64748b">
          {observations[observations.length - 1]?.observed_on}
        </text>
      </svg>
    </div>
  );
}

// MacroDashboard loads dataset definitions first and observations second. This
// two-step flow prevents the UI from requesting a series code that the backend
// has not advertised as an active dataset.
export default function MacroDashboard() {
  const [datasets, setDatasets] = useState<MacroDataset[]>([]);
  const [selectedCode, setSelectedCode] = useState("");
  const [from, setFrom] = useState("2026-01-01");
  const [to, setTo] = useState("2026-03-01");
  const [observations, setObservations] = useState<MacroObservation[]>([]);
  const [isLoadingDatasets, setIsLoadingDatasets] = useState(true);
  const [isLoadingObservations, setIsLoadingObservations] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load dataset definitions once when the component mounts.
  useEffect(() => {
    const controller = new AbortController();
    setIsLoadingDatasets(true);

    listMacroDatasets(controller.signal)
      .then((loadedDatasets) => {
        setDatasets(loadedDatasets);
        setSelectedCode((currentCode) => currentCode || loadedDatasets[0]?.code || "");
      })
      .catch((requestError: unknown) => {
        if (requestError instanceof DOMException && requestError.name === "AbortError") {
          return;
        }
        setError(requestError instanceof Error ? requestError.message : "Unable to load macro datasets");
      })
      .finally(() => setIsLoadingDatasets(false));

    return () => controller.abort();
  }, []);

  const selectedDataset = useMemo(
    () => datasets.find((dataset) => dataset.code === selectedCode),
    [datasets, selectedCode]
  );

  // Fetch observations whenever the selected series or visible date range changes.
  useEffect(() => {
    if (!selectedCode || !from || !to) {
      return;
    }

    const controller = new AbortController();
    setIsLoadingObservations(true);
    setError(null);

    listMacroObservations(selectedCode, from, to, controller.signal)
      .then(setObservations)
      .catch((requestError: unknown) => {
        if (requestError instanceof DOMException && requestError.name === "AbortError") {
          return;
        }
        setObservations([]);
        setError(requestError instanceof Error ? requestError.message : "Unable to load macro observations");
      })
      .finally(() => setIsLoadingObservations(false));

    return () => controller.abort();
  }, [from, selectedCode, to]);

  const isLoading = isLoadingDatasets || isLoadingObservations;

  return (
    <section className="mt-10">
      <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
        <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <p className="text-sm font-semibold uppercase tracking-[0.18em] text-growth">Macro view</p>
            <h2 className="mt-2 text-2xl font-semibold text-ink">Macroeconomic indicators</h2>
            <p className="mt-1 text-sm text-slate-500">Inspect CPI and policy-rate observations over time.</p>
          </div>

          <div className="grid gap-4 sm:grid-cols-3">
            <label className="text-sm font-medium text-slate-700">
              Dataset
              <select
                value={selectedCode}
                onChange={(event) => setSelectedCode(event.target.value)}
                className="mt-1 block w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-normal text-slate-800"
              >
                {datasets.map((dataset) => (
                  <option key={dataset.id} value={dataset.code}>
                    {dataset.name}
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

        {selectedDataset && (
          <div className="mt-5 flex flex-col gap-2 border-t border-slate-100 pt-4 text-xs text-slate-500 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-5">
            <span>Provider: <strong className="font-medium text-slate-700">{selectedDataset.provider}</strong></span>
            <span>Unit: <strong className="font-medium text-slate-700">{selectedDataset.unit}</strong></span>
            <span>Frequency: <strong className="font-medium text-slate-700">{selectedDataset.frequency}</strong></span>
            <a href={selectedDataset.source_url} target="_blank" rel="noreferrer" className="font-medium text-growth underline">
              View source
            </a>
          </div>
        )}
      </div>

      {isLoading ? (
        <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-8 text-slate-600 shadow-sm">Loading macro observations…</div>
      ) : error ? (
        <div className="mt-6 rounded-2xl border border-amber-200 bg-amber-50 p-6 text-amber-900">
          <h3 className="font-semibold">Unable to load macro data</h3>
          <p className="mt-2 text-sm">Check that the Go API is running and that the selected date range is valid.</p>
          <p className="mt-2 text-xs">Technical detail: {error}</p>
        </div>
      ) : observations.length === 0 ? (
        <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-8 text-slate-600 shadow-sm">No observations were found for this selection.</div>
      ) : (
        <div className="mt-6 rounded-2xl border border-slate-200 bg-white p-6 shadow-sm">
          <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
            <div>
              <h3 className="text-xl font-semibold text-ink">{selectedDataset?.name}</h3>
              <p className="mt-1 text-sm text-slate-500">Latest value: {observations[observations.length - 1]?.value} {selectedDataset?.unit}</p>
            </div>
            <p className="text-sm text-slate-500">{observations.length} observation{observations.length === 1 ? "" : "s"}</p>
          </div>
          <MacroLineChart observations={observations} unit={selectedDataset?.unit ?? "unit"} />
        </div>
      )}
    </section>
  );
}

