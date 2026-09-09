package ingestion_test

import (
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestLiveMonitorStatusRegistryKeepsProviderSymbolsIndependent(t *testing.T) {
	registry := ingestion.NewLiveMonitorStatusRegistry()
	binance := registry.Register(ingestion.InstrumentReference{
		CanonicalSymbol: "BTCUSDT",
		Provider:        ingestion.ProviderBinance,
		ProviderSymbol:  "BTCUSDT",
	})
	yahoo := registry.Register(ingestion.InstrumentReference{
		CanonicalSymbol: "SBIN",
		Provider:        ingestion.ProviderYahoo,
		ProviderSymbol:  "SBIN.NS",
	})

	if binance == yahoo {
		t.Fatal("different provider symbols share one status store")
	}
	binance.RecordReconnect()
	yahoo.RecordAccepted(LiveMarketEventForRegistryTest(t))

	statuses := registry.List()
	if len(statuses) != 2 {
		t.Fatalf("status count = %d, want 2", len(statuses))
	}
	if statuses[0].Provider != "binance" || statuses[0].ProviderSymbol != "BTCUSDT" {
		t.Fatalf("first status = %#v, want Binance BTCUSDT", statuses[0])
	}
	if statuses[0].Reconnects != 1 || statuses[1].Accepted != 1 {
		t.Fatalf("status counters crossed targets: %#v", statuses)
	}
}

// LiveMarketEventForRegistryTest supplies a valid event without coupling this
// registry test to the provider-specific WebSocket fixtures.
func LiveMarketEventForRegistryTest(t *testing.T) ingestion.LiveMarketEvent {
	t.Helper()
	var price, quantity pgtype.Numeric
	if err := price.Scan("100"); err != nil {
		t.Fatalf("scan price: %v", err)
	}
	if err := quantity.Scan("1"); err != nil {
		t.Fatalf("scan quantity: %v", err)
	}
	now := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
	return ingestion.LiveMarketEvent{
		Provider:         ingestion.ProviderYahoo,
		ProviderSymbol:   "SBIN.NS",
		EventType:        "trade",
		ObservedAt:       now,
		Price:            price,
		Quantity:         quantity,
		SourceReceivedAt: now,
	}
}
