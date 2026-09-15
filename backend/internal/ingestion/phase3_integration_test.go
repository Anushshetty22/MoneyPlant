package ingestion_test

import (
	"context"
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestPhase3BinanceFlowReachesSnapshotAndCandle verifies the complete
// provider-neutral path used by Binance: normalized live events are validated
// by the monitor, retained as the latest snapshot, and rolled into one-minute
// OHLCV state.
func TestPhase3BinanceFlowReachesSnapshotAndCandle(t *testing.T) {
	snapshot, candle := runPhase3ProviderFlow(t, ingestion.ProviderBinance, "BTCUSDT", "BTCUSDT", 101)
	assertPhase3Flow(t, snapshot, candle, "BTCUSDT", ingestion.ProviderBinance, "BTCUSDT")
}

// TestPhase3AngelOneFlowReachesSnapshotAndCandle verifies the same end-to-end
// normalized path for an Angel One equity, including the distinction between
// canonical symbol TCS and provider symbol TCS-EQ.
func TestPhase3AngelOneFlowReachesSnapshotAndCandle(t *testing.T) {
	snapshot, candle := runPhase3ProviderFlow(t, ingestion.ProviderAngelOne, "TCS", "TCS-EQ", 202)
	assertPhase3Flow(t, snapshot, candle, "TCS", ingestion.ProviderAngelOne, "TCS-EQ")
}

func runPhase3ProviderFlow(t *testing.T, provider ingestion.ProviderID, canonicalSymbol, providerSymbol string, sourceID int64) (ingestion.LiveMarketEvent, phase3Candle) {
	t.Helper()
	start := time.Date(2026, time.September, 15, 10, 0, 10, 0, time.UTC)
	events := []ingestion.LiveMarketEvent{
		phase3LiveEvent(t, provider, canonicalSymbol, providerSymbol, "100.00", "1.00", start),
		phase3LiveEvent(t, provider, canonicalSymbol, providerSymbol, "101.00", "2.00", start.Add(25*time.Second)),
	}

	providerAdapter := ingestion.NewFixtureLiveMarketDataProvider(events)
	stream, err := providerAdapter.OpenTradeStream(context.Background(), ingestion.LiveMarketStreamRequest{
		CanonicalSymbol: canonicalSymbol,
		ProviderSymbol:  providerSymbol,
	})
	if err != nil {
		t.Fatalf("open %s fixture stream: %v", provider, err)
	}
	monitor, err := ingestion.NewLiveMarketMonitor(stream)
	if err != nil {
		t.Fatalf("create %s monitor: %v", provider, err)
	}

	snapshotStore := ingestion.NewLiveMarketSnapshotStore()
	aggregator := ingestion.NewLiveMinuteCandleAggregator()
	result, err := monitor.Run(context.Background(), func(ctx context.Context, event ingestion.LiveMarketEvent) error {
		if err := snapshotStore.Handle(ctx, event); err != nil {
			return err
		}
		_, err := aggregator.Add(event, sourceID)
		return err
	})
	if err != nil {
		t.Fatalf("run %s monitor: %v", provider, err)
	}
	if result.Received != 2 || result.Accepted != 2 || result.Rejected != 0 {
		t.Fatalf("%s result = %#v, want received=2 accepted=2 rejected=0", provider, result)
	}

	snapshots := snapshotStore.ListFiltered(string(provider), canonicalSymbol)
	if len(snapshots) != 1 {
		t.Fatalf("%s snapshots = %d, want 1", provider, len(snapshots))
	}
	closed := aggregator.FlushClosed(start.Add(time.Minute))
	if len(closed) != 1 {
		t.Fatalf("%s closed candles = %d, want 1", provider, len(closed))
	}
	return snapshots[0], phase3Candle{
		open:       closed[0].Open,
		high:       closed[0].High,
		low:        closed[0].Low,
		close:      closed[0].Close,
		volume:     closed[0].Volume,
		tradeCount: closed[0].TradeCount.Int64,
	}
}

type phase3Candle struct {
	open       pgtype.Numeric
	high       pgtype.Numeric
	low        pgtype.Numeric
	close      pgtype.Numeric
	volume     pgtype.Numeric
	tradeCount int64
}

func assertPhase3Flow(t *testing.T, snapshot ingestion.LiveMarketEvent, candle phase3Candle, canonicalSymbol string, provider ingestion.ProviderID, providerSymbol string) {
	t.Helper()
	if snapshot.CanonicalSymbol != canonicalSymbol || snapshot.Provider != provider || snapshot.ProviderSymbol != providerSymbol {
		t.Fatalf("snapshot identity = %#v, want canonical=%s provider=%s provider symbol=%s", snapshot, canonicalSymbol, provider, providerSymbol)
	}
	if numericText(snapshot.Price) != "101.00" {
		t.Fatalf("latest %s price = %s, want 101.00", canonicalSymbol, numericText(snapshot.Price))
	}
	if numericText(candle.open) != "100.00" || numericText(candle.high) != "101.00" || numericText(candle.low) != "100.00" || numericText(candle.close) != "101.00" {
		t.Fatalf("%s candle OHLC = %s/%s/%s/%s, want 100/101/100/101", canonicalSymbol, numericText(candle.open), numericText(candle.high), numericText(candle.low), numericText(candle.close))
	}
	if numericText(candle.volume) != "3.00" || candle.tradeCount != 2 {
		t.Fatalf("%s candle volume/trades = %s/%d, want 3.00/2", canonicalSymbol, numericText(candle.volume), candle.tradeCount)
	}
}

func phase3LiveEvent(t *testing.T, provider ingestion.ProviderID, canonicalSymbol, providerSymbol, price, quantity string, observedAt time.Time) ingestion.LiveMarketEvent {
	t.Helper()
	return ingestion.LiveMarketEvent{
		CanonicalSymbol:  canonicalSymbol,
		Provider:         provider,
		ProviderSymbol:   providerSymbol,
		EventType:        "trade",
		ObservedAt:       observedAt,
		Price:            phase3Numeric(t, price),
		Quantity:         phase3Numeric(t, quantity),
		SourceReceivedAt: observedAt.Add(250 * time.Millisecond),
	}
}

func phase3Numeric(t *testing.T, value string) pgtype.Numeric {
	t.Helper()
	var numeric pgtype.Numeric
	if err := numeric.Scan(value); err != nil {
		t.Fatalf("parse numeric %q: %v", value, err)
	}
	return numeric
}

func numericText(value pgtype.Numeric) string {
	text, err := value.Value()
	if err != nil {
		return "<invalid>"
	}
	return text.(string)
}
