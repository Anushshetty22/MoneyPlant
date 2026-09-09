"use client";

import { useEffect, useMemo, useState } from "react";
import {
  getLiveMonitorStatus,
  listCandles,
  listLiveSnapshots,
  type Candle,
  type Instrument,
  type LiveMonitorStatus,
  type LiveSnapshot
} from "@/lib/api";

// The Phase 1 source decision currently maps crypto to Binance and equities to
// Yahoo Finance. This small resolver keeps that initial rule visible; a future
// instrument-source API can replace it when more providers are added.
function providerForInstrument(instrument: Instrument): string {
  return instrument.asset_type === "crypto" ? "binance" : "yahoo";
}

// formatRetrievedAt turns the machine timestamp from the API into a readable
// local-time label. The original UTC value remains available in the HTML title
// attribute for users who need the exact retrieval instant.
function formatRetrievedAt(value: string): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short"
  }).format(new Date(value));
}

// formatLiveTimestamp makes the browser's local timezone visible to the user.
// The exact UTC value remains available through the title attribute, which is
// useful when comparing the dashboard with provider timestamps and logs.
function formatLiveTimestamp(value: string): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium"
  }).format(new Date(value));
}

// A snapshot older than three polling intervals is treated as stale. This is
// not a trading rule; it is a simple operational signal that tells us the
// backend may no longer be receiving provider events.
const liveStaleAfterMilliseconds = 15_000;

function formatLiveAge(value: string): string {
  const ageMilliseconds = Date.now() - new Date(value).getTime();
  if (!Number.isFinite(ageMilliseconds)) {
    return "an unknown amount of time";
  }
  const ageSeconds = Math.floor(ageMilliseconds / 1000);
  if (ageSeconds < 1) {
    return "just now";
  }
  if (ageSeconds < 60) {
    return `${ageSeconds}s ago`;
  }
  return `${Math.floor(ageSeconds / 60)}m ago`;
}

// LiveSnapshotCard displays the latest event separately from historical candles
// because the two values have different meanings: a candle summarizes a time
// window, while this card shows the most recent individual trade.
function LiveSnapshotCard({
  symbol,
  isSupported,
  snapshot,
  status,
  isLoading,
  error
}: {
  symbol: string;
  isSupported: boolean;
  snapshot: LiveSnapshot | null;
  status: LiveMonitorStatus | null;
  isLoading: boolean;
  error: string | null;
}) {
  const receivedAtMilliseconds = snapshot ? new Date(snapshot.source_received_at).getTime() : NaN;
  const isStale = snapshot
    ? !Number.isFinite(receivedAtMilliseconds) || Date.now() - receivedAtMilliseconds > liveStaleAfterMilliseconds
    : false;
  const stateLabel = status?.state ?? "unknown";
  const hasMonitorError = stateLabel === "error";
  const isReconnecting = stateLabel === "reconnecting";
  const isDisabled = stateLabel === "disabled";
  const statusDotClass = hasMonitorError
    ? "bg-red-500"
    : isReconnecting
      ? "bg-amber-500"
      : isDisabled
        ? "bg-slate-300"
      : isStale
        ? "bg-amber-500"
      : stateLabel === "running"
        ? "bg-emerald-500"
        : "bg-slate-300";
  const statusLabel = hasMonitorError
    ? "Monitor error"
    : isReconnecting
      ? "Reconnecting"
      : isDisabled
        ? "Disabled"
      : isStale
      ? "Stale data"
      : status?.state === "running"
        ? "Live"
        : status?.state ?? "Waiting for status";

  return (
    <div className="mt-6 rounded-2xl border border-teal-100 bg-teal-50/60 p-6 shadow-sm">
      <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
        <div>
          <p className="text-sm font-semibold uppercase tracking-[0.18em] text-growth">Live monitor</p>
          <h3 className="mt-2 text-xl font-semibold text-ink">Latest trade</h3>
          <p className="mt-1 text-sm text-slate-600">Refreshes every five seconds while this instrument is selected.</p>
        </div>
        <span className="inline-flex items-center gap-2 text-xs font-medium text-slate-600">
          <span className={`h-2.5 w-2.5 rounded-full ${statusDotClass}`} />
          {statusLabel}
        </span>
      </div>

      {!isSupported ? (
        <p className="mt-5 text-sm text-slate-600">
          Live Binance monitoring is currently available for crypto instruments only. Historical data remains available below.
        </p>
      ) : isLoading && !snapshot ? (
        <p className="mt-5 text-sm text-slate-600">Checking the live stream…</p>
      ) : error ? (
        <p className="mt-5 text-sm text-amber-800">Unable to load live data: {error}</p>
      ) : hasMonitorError && status?.last_error ? (
        <p className="mt-5 text-sm text-red-800">Monitor error: {status.last_error}</p>
      ) : snapshot ? (
        <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <p className="text-xs uppercase tracking-wide text-slate-500">Price</p>
            <p className="mt-1 text-2xl font-semibold text-ink">{snapshot.price}</p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wide text-slate-500">Quantity</p>
            <p className="mt-1 text-lg font-medium text-ink">{snapshot.quantity}</p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wide text-slate-500">Trade time</p>
            <p className="mt-1 text-sm font-medium text-ink" title={snapshot.observed_at}>
              {formatLiveTimestamp(snapshot.observed_at)}
            </p>
          </div>
          <div>
            <p className="text-xs uppercase tracking-wide text-slate-500">Received by backend</p>
            <p className="mt-1 text-sm font-medium text-ink" title={snapshot.source_received_at}>
              {formatLiveTimestamp(snapshot.source_received_at)}
            </p>
            <p className={`mt-1 text-xs ${isStale ? "text-amber-800" : "text-slate-500"}`}>
              {isStale ? `No update for ${formatLiveAge(snapshot.source_received_at)}` : formatLiveAge(snapshot.source_received_at)}
            </p>
          </div>
        </div>
      ) : (
        <p className="mt-5 text-sm text-slate-600">
          No live snapshot is available. Start the API with <code className="rounded bg-white px-1 py-0.5">LIVE_MONITOR_SYMBOL={symbol}</code> to enable it.
        </p>
      )}

      {isSupported && status && (
        <div className="mt-5 flex flex-wrap gap-x-5 gap-y-2 border-t border-teal-100 pt-4 text-xs text-slate-600">
          <span>Received: <strong className="font-medium text-slate-800">{status.received}</strong></span>
          <span>Accepted: <strong className="font-medium text-slate-800">{status.accepted}</strong></span>
          <span>Rejected: <strong className="font-medium text-slate-800">{status.rejected}</strong></span>
          <span>Reconnects: <strong className="font-medium text-slate-800">{status.reconnects}</strong></span>
          <span>Saved: <strong className="font-medium text-slate-800">{status.persisted}</strong></span>
          <span>Restored: <strong className="font-medium text-slate-800">{status.restored}</strong></span>
          {status.last_persisted_at && (
            <span title={status.last_persisted_at}>Last saved: <strong className="font-medium text-slate-800">{formatLiveTimestamp(status.last_persisted_at)}</strong></span>
          )}
          <span title={status.updated_at}>Status updated: <strong className="font-medium text-slate-800">{formatLiveTimestamp(status.updated_at)}</strong></span>
        </div>
      )}

      {isSupported && status?.last_persistence_error && (
        <p className="mt-3 text-xs text-amber-800">Database save warning: {status.last_persistence_error}</p>
      )}
      {isSupported && status?.last_reconnect_error && (
        <p className="mt-3 text-xs text-amber-800">Reconnect detail: {status.last_reconnect_error}</p>
      )}
    </div>
  );
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
  const [liveSnapshot, setLiveSnapshot] = useState<LiveSnapshot | null>(null);
  const [liveStatus, setLiveStatus] = useState<LiveMonitorStatus | null>(null);
  const [isLiveLoading, setIsLiveLoading] = useState(false);
  const [liveError, setLiveError] = useState<string | null>(null);

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

  useEffect(() => {
    setLiveSnapshot(null);
    setLiveStatus(null);
    setLiveError(null);

    if (!selectedSymbol || selectedInstrument?.asset_type !== "crypto") {
      setIsLiveLoading(false);
      return;
    }

    let isCurrentRequest = true;
    const controller = new AbortController();

    // Fetch once immediately and then poll. The API currently exposes a
    // snapshot endpoint rather than a browser WebSocket, so polling keeps this
    // first dashboard integration simple while the backend stream stays live.
    const loadLiveSnapshot = () => {
      setIsLiveLoading(true);
      setLiveError(null);

      Promise.all([
        listLiveSnapshots(selectedSymbol, controller.signal),
        getLiveMonitorStatus(controller.signal)
      ])
        .then(([snapshot, status]) => {
          if (isCurrentRequest) {
            setLiveSnapshot(snapshot);
            setLiveStatus(status);
          }
        })
        .catch((requestError: unknown) => {
          if (!isCurrentRequest || (requestError instanceof DOMException && requestError.name === "AbortError")) {
            return;
          }
          setLiveError(requestError instanceof Error ? requestError.message : "Unable to load live data");
        })
        .finally(() => {
          if (isCurrentRequest) {
            setIsLiveLoading(false);
          }
        });
    };

    loadLiveSnapshot();
    const refreshTimer = window.setInterval(loadLiveSnapshot, 5000);

    return () => {
      isCurrentRequest = false;
      controller.abort();
      window.clearInterval(refreshTimer);
    };
  }, [selectedInstrument, selectedSymbol]);

  return (
    <section id="market-view" className="mt-10 scroll-mt-20">
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
          <div className="mt-5 flex flex-col gap-2 border-t border-slate-100 pt-4 text-xs text-slate-500 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-5">
            <span>Source: <strong className="font-medium text-slate-700">{providerForInstrument(selectedInstrument)}</strong></span>
            <span>Interval: <strong className="font-medium text-slate-700">1d</strong></span>
            <span>Times are UTC</span>
            {candles.length > 0 && (
              <span title={candles[candles.length - 1].source_retrieved_at}>
                Retrieved: <strong className="font-medium text-slate-700">{formatRetrievedAt(candles[candles.length - 1].source_retrieved_at)}</strong>
              </span>
            )}
          </div>
        )}
      </div>

      <LiveSnapshotCard
        symbol={selectedSymbol}
        isSupported={selectedInstrument?.asset_type === "crypto"}
        snapshot={liveSnapshot}
        status={liveStatus}
        isLoading={isLiveLoading}
        error={liveError}
      />

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
