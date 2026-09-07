package ingestion_test

import (
	// context supplies the monitor lifecycle context used by every stream.
	"context"
	// encoding/json checks the numeric value through its exact JSON boundary
	// rather than depending on pgtype.Numeric's internal representation.
	"encoding/json"
	// testing provides the unit-test assertions.
	"testing"
	// time gives the fixture events deterministic provider and receive times.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestFixtureLiveStreamDeliversNormalizedEvents verifies the first Phase 2
// boundary: a provider-specific source can be consumed through the generic
// stream interface and valid events reach the injected callback.
func TestFixtureLiveStreamDeliversNormalizedEvents(t *testing.T) {
	provider := ingestion.NewFixtureLiveMarketDataProvider([]ingestion.LiveMarketEvent{
		liveEvent(t, "BTCUSDT", "64323.61000000", "0.01000000", 0),
		liveEvent(t, "BTCUSDT", "64324.12000000", "0.02000000", 1),
		liveEvent(t, "SBIN.NS", "812.50000000", "10.00000000", 2),
	})

	stream, err := provider.OpenTradeStream(context.Background(), ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTCUSDT"})
	if err != nil {
		t.Fatalf("open fixture stream: %v", err)
	}
	monitor, err := ingestion.NewLiveMarketMonitor(stream)
	if err != nil {
		t.Fatalf("create live market monitor: %v", err)
	}

	var handled []ingestion.LiveMarketEvent
	result, err := monitor.Run(context.Background(), func(_ context.Context, event ingestion.LiveMarketEvent) error {
		handled = append(handled, event)
		return nil
	})
	if err != nil {
		t.Fatalf("run live market monitor: %v", err)
	}

	if result.Received != 2 || result.Accepted != 2 || result.Rejected != 0 {
		t.Fatalf("result = %#v, want received=2 accepted=2 rejected=0", result)
	}
	if len(handled) != 2 {
		t.Fatalf("handled events = %d, want 2", len(handled))
	}
	if result.LastEvent == nil || !result.LastEvent.Price.Valid {
		t.Fatal("monitor did not retain the last valid event")
	}
	encodedPrice, err := json.Marshal(result.LastEvent.Price)
	if err != nil {
		t.Fatalf("marshal last price: %v", err)
	}
	if string(encodedPrice) != "64324.12000000" {
		t.Fatalf("last price = %s, want exact decimal 64324.12000000", encodedPrice)
	}
}

// TestLiveMarketMonitorRejectsOneBadEventAndContinues verifies the live-stream
// safety rule that malformed provider messages are counted and skipped while
// later valid messages continue through the monitor.
func TestLiveMarketMonitorRejectsOneBadEventAndContinues(t *testing.T) {
	validBefore := liveEvent(t, "BTCUSDT", "100.00", "1.00", 0)
	invalid := liveEvent(t, "BTCUSDT", "101.00", "-1.00", 1)
	validAfter := liveEvent(t, "BTCUSDT", "102.00", "2.00", 2)

	provider := ingestion.NewFixtureLiveMarketDataProvider([]ingestion.LiveMarketEvent{validBefore, invalid, validAfter})
	stream, err := provider.OpenTradeStream(context.Background(), ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTCUSDT"})
	if err != nil {
		t.Fatalf("open fixture stream: %v", err)
	}
	monitor, err := ingestion.NewLiveMarketMonitor(stream)
	if err != nil {
		t.Fatalf("create live market monitor: %v", err)
	}

	handled := 0
	result, err := monitor.Run(context.Background(), func(_ context.Context, _ ingestion.LiveMarketEvent) error {
		handled++
		return nil
	})
	if err != nil {
		t.Fatalf("run live market monitor: %v", err)
	}

	if result.Received != 3 || result.Accepted != 2 || result.Rejected != 1 {
		t.Fatalf("result = %#v, want received=3 accepted=2 rejected=1", result)
	}
	if handled != 2 {
		t.Fatalf("handled events = %d, want 2", handled)
	}
}

// liveEvent creates the exact pgtype.Numeric values used by the event contract.
// The helper stays in the test package so production callers learn that source
// adapters must parse decimal strings explicitly before creating events.
func liveEvent(t *testing.T, symbol, price, quantity string, offset int) ingestion.LiveMarketEvent {
	t.Helper()
	priceValue := liveNumeric(t, price)
	quantityValue := liveNumeric(t, quantity)
	observedAt := time.Date(2026, time.August, 12, 10, 0, offset, 0, time.UTC)
	return ingestion.LiveMarketEvent{
		ProviderSymbol:   symbol,
		EventType:        "trade",
		ObservedAt:       observedAt,
		Price:            priceValue,
		Quantity:         quantityValue,
		SourceReceivedAt: observedAt.Add(250 * time.Millisecond),
	}
}

func liveNumeric(t *testing.T, value string) pgtype.Numeric {
	t.Helper()
	var result pgtype.Numeric
	if err := result.Scan(value); err != nil {
		t.Fatalf("parse test decimal %q: %v", value, err)
	}
	return result
}
