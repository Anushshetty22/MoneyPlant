package ingestion_test

import (
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestLiveMinuteCandleAggregatorHandlesOutOfOrderEvents(t *testing.T) {
	aggregator := ingestion.NewLiveMinuteCandleAggregator()
	base := time.Date(2026, time.September, 13, 10, 0, 0, 0, time.UTC)

	// Arrival order differs from event-time order: the second event is the
	// close, while the third event is the earlier open.
	for _, event := range []ingestion.LiveMarketEvent{
		liveCandleEvent(t, base.Add(40*time.Second), "100", "1"),
		liveCandleEvent(t, base.Add(20*time.Second), "110", "2"),
		liveCandleEvent(t, base.Add(10*time.Second), "95", "3"),
	} {
		if _, err := aggregator.Add(event, 42); err != nil {
			t.Fatalf("add live event: %v", err)
		}
	}

	candles := aggregator.Flush()
	if len(candles) != 1 {
		t.Fatalf("flushed candles = %d, want 1", len(candles))
	}
	candle := candles[0]
	if candle.InstrumentSourceID != 42 || candle.Interval != "1m" || candle.TradeCount.Int64 != 3 {
		t.Fatalf("candle identity/metrics = %#v", candle)
	}
	assertCandleNumeric(t, candle.Open, "95")
	assertCandleNumeric(t, candle.High, "110")
	assertCandleNumeric(t, candle.Low, "95")
	assertCandleNumeric(t, candle.Close, "100")
	assertCandleNumeric(t, candle.Volume, "6")
	if candle.ObservedAt.Time != base {
		t.Fatalf("bucket = %s, want %s", candle.ObservedAt.Time, base)
	}
}

func TestLiveMinuteCandleAggregatorFlushesClosedBucketsOnce(t *testing.T) {
	aggregator := ingestion.NewLiveMinuteCandleAggregator()
	base := time.Date(2026, time.September, 13, 10, 0, 0, 0, time.UTC)
	if _, err := aggregator.Add(liveCandleEvent(t, base.Add(5*time.Second), "10", "1"), 7); err != nil {
		t.Fatalf("add first event: %v", err)
	}
	if _, err := aggregator.Add(liveCandleEvent(t, base.Add(time.Minute+5*time.Second), "11", "2"), 7); err != nil {
		t.Fatalf("add second event: %v", err)
	}
	closed := aggregator.FlushClosed(base.Add(time.Minute + 10*time.Second))
	if len(closed) != 1 {
		t.Fatalf("closed candle count = %d, want 1", len(closed))
	}
	if retry := aggregator.FlushClosed(base.Add(90 * time.Second)); len(retry) != 1 {
		t.Fatalf("unacknowledged retry count = %d, want 1", len(retry))
	} else {
		aggregator.MarkPersisted(retry[0])
	}
	if next := aggregator.FlushClosed(base.Add(2 * time.Minute)); len(next) != 1 {
		t.Fatalf("next closed candle count = %d, want 1", len(next))
	} else {
		aggregator.MarkPersisted(next[0])
	}
	if remaining := aggregator.Flush(); len(remaining) != 0 {
		t.Fatalf("remaining candle count = %d, want 0", len(remaining))
	}
}

func TestLiveMinuteCandleAggregatorRevisesRecentlyFlushedBucket(t *testing.T) {
	aggregator := ingestion.NewLiveMinuteCandleAggregator()
	base := time.Date(2026, time.September, 13, 10, 0, 0, 0, time.UTC)
	if _, err := aggregator.Add(liveCandleEvent(t, base.Add(50*time.Second), "100", "1"), 7); err != nil {
		t.Fatalf("add initial event: %v", err)
	}
	if flushed := aggregator.FlushClosed(base.Add(time.Minute)); len(flushed) != 1 {
		t.Fatalf("initial flush count = %d, want 1", len(flushed))
	} else {
		aggregator.MarkPersisted(flushed[0])
	}
	if _, err := aggregator.Add(liveCandleEvent(t, base.Add(10*time.Second), "90", "2"), 7); err != nil {
		t.Fatalf("add late event: %v", err)
	}
	updated := aggregator.FlushClosed(base.Add(time.Minute + 10*time.Second))
	if len(updated) != 1 {
		t.Fatalf("late update flush count = %d, want 1", len(updated))
	}
	assertCandleNumeric(t, updated[0].Open, "90")
	assertCandleNumeric(t, updated[0].Close, "100")
	assertCandleNumeric(t, updated[0].Volume, "3")
	aggregator.MarkPersisted(updated[0])
}

func liveCandleEvent(t *testing.T, observedAt time.Time, price, quantity string) ingestion.LiveMarketEvent {
	t.Helper()
	return ingestion.LiveMarketEvent{
		Provider:         ingestion.ProviderAngelOne,
		ProviderSymbol:   "Nifty 50",
		EventType:        "trade",
		ObservedAt:       observedAt,
		Price:            liveCandleNumeric(t, price),
		Quantity:         liveCandleNumeric(t, quantity),
		SourceReceivedAt: observedAt.Add(time.Millisecond),
	}
}

func liveCandleNumeric(t *testing.T, value string) pgtype.Numeric {
	t.Helper()
	var numeric pgtype.Numeric
	if err := numeric.Scan(value); err != nil {
		t.Fatalf("scan numeric %q: %v", value, err)
	}
	return numeric
}

func assertCandleNumeric(t *testing.T, value pgtype.Numeric, want string) {
	t.Helper()
	var expected pgtype.Numeric
	if err := expected.Scan(want); err != nil {
		t.Fatalf("scan expected numeric %q: %v", want, err)
	}
	if value.Int.Cmp(expected.Int) != 0 || value.Exp != expected.Exp {
		t.Fatalf("numeric = %#v, want %s", value, want)
	}
}
