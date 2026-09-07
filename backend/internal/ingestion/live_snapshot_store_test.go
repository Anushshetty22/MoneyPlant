package ingestion_test

import (
	// context is passed to the store's event-handler method.
	"context"
	// sync coordinates concurrent writers and readers in the safety test.
	"sync"
	// testing provides assertions.
	"testing"
	// time creates deterministic event ordering.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// TestLiveMarketSnapshotStoreKeepsLatestEvent verifies the main snapshot rule:
// one symbol has one current event, and a newer event replaces the old one.
func TestLiveMarketSnapshotStoreKeepsLatestEvent(t *testing.T) {
	store := ingestion.NewLiveMarketSnapshotStore()
	first := reconnectEvent(t)
	second := first
	second.Price = liveNumeric(t, "101.00")
	second.SourceReceivedAt = second.SourceReceivedAt.Add(time.Second)
	other := first
	other.ProviderSymbol = "SBIN.NS"

	for _, event := range []ingestion.LiveMarketEvent{first, second, other} {
		if err := store.Handle(context.Background(), event); err != nil {
			t.Fatalf("store event: %v", err)
		}
	}

	if store.Count() != 2 {
		t.Fatalf("store count = %d, want 2", store.Count())
	}
	latest, exists := store.Get("btcusdt")
	if !exists {
		t.Fatal("expected BTCUSDT snapshot")
	}
	if got := latest.Price.Int.String(); got != "10100" {
		t.Fatalf("latest price integer = %s, want 10100", got)
	}
	if len(store.List()) != 2 {
		t.Fatalf("snapshot list length = %d, want 2", len(store.List()))
	}
}

// TestLiveMarketSnapshotStoreRejectsInvalidEvents verifies that direct callers
// cannot bypass the common event validation contract.
func TestLiveMarketSnapshotStoreRejectsInvalidEvents(t *testing.T) {
	store := ingestion.NewLiveMarketSnapshotStore()
	invalid := reconnectEvent(t)
	invalid.Quantity = liveNumeric(t, "0")

	if err := store.Handle(context.Background(), invalid); err == nil {
		t.Fatal("expected invalid quantity error, got nil")
	}
	if store.Count() != 0 {
		t.Fatalf("store count = %d, want 0 after rejection", store.Count())
	}
}

// TestLiveMarketSnapshotStoreSupportsConcurrentAccess exercises the read/write
// locks with many callbacks and reads. Run this test with -race for the most
// useful race-detector feedback.
func TestLiveMarketSnapshotStoreSupportsConcurrentAccess(t *testing.T) {
	store := ingestion.NewLiveMarketSnapshotStore()
	var waitGroup sync.WaitGroup

	for writer := 0; writer < 20; writer++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			event := reconnectEvent(t)
			event.ProviderSymbol = "BTCUSDT"
			event.Price = liveNumeric(t, "100")
			if index%2 == 0 {
				event.ProviderSymbol = "SBIN.NS"
			}
			if err := store.Handle(context.Background(), event); err != nil {
				t.Errorf("concurrent store event: %v", err)
			}
		}(writer)
	}

	for reader := 0; reader < 20; reader++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_ = store.Count()
			_ = store.List()
			_, _ = store.Get("BTCUSDT")
		}()
	}

	waitGroup.Wait()
	if store.Count() != 2 {
		t.Fatalf("store count = %d, want 2 after concurrent access", store.Count())
	}
}
