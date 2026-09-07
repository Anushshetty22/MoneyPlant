package ingestion

import (
	// context keeps the store compatible with LiveMarketEventHandler and allows
	// future persistence work to observe cancellation consistently.
	"context"
	// fmt reports invalid snapshot requests with a useful operation name.
	"fmt"
	// math/big lets us copy pgtype.Numeric's internal integer safely.
	"math/big"
	// sort makes List return deterministic output for APIs, logs, and tests.
	"sort"
	// strings normalizes symbol keys consistently on writes and reads.
	"strings"
	// sync protects the snapshot map from concurrent stream callbacks and API
	// readers.
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
)

// LiveMarketSnapshotStore keeps the latest valid event for each provider symbol.
//
// This is intentionally an in-memory Phase 2.4 component. It gives the live
// monitor somewhere useful to write while we learn concurrency and snapshot
// semantics before designing a durable live-tick table or API contract.
type LiveMarketSnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[string]LiveMarketEvent
}

// NewLiveMarketSnapshotStore creates an empty snapshot store.
func NewLiveMarketSnapshotStore() *LiveMarketSnapshotStore {
	return &LiveMarketSnapshotStore{
		snapshots: make(map[string]LiveMarketEvent),
	}
}

// Handle implements LiveMarketEventHandler and stores one valid event.
//
// The monitor already validates events, but validating again at this boundary
// protects the store if another caller uses it directly in the future.
func (s *LiveMarketSnapshotStore) Handle(_ context.Context, event LiveMarketEvent) error {
	if err := ValidateLiveMarketEvent(event); err != nil {
		return fmt.Errorf("store live market snapshot: %w", err)
	}

	symbol := normalizeSnapshotSymbol(event.ProviderSymbol)
	s.mu.Lock()
	s.snapshots[symbol] = cloneLiveMarketEvent(event)
	s.mu.Unlock()
	return nil
}

// Get returns the latest event for one symbol. The boolean is false when no
// event has been stored for that symbol yet.
func (s *LiveMarketSnapshotStore) Get(providerSymbol string) (LiveMarketEvent, bool) {
	symbol := normalizeSnapshotSymbol(providerSymbol)
	s.mu.RLock()
	event, exists := s.snapshots[symbol]
	s.mu.RUnlock()
	if !exists {
		return LiveMarketEvent{}, false
	}
	return cloneLiveMarketEvent(event), true
}

// List returns one latest event per symbol in sorted symbol order.
//
// Returning copies means callers can inspect or serialize the result without
// holding the store's read lock and without changing the stored values.
func (s *LiveMarketSnapshotStore) List() []LiveMarketEvent {
	s.mu.RLock()
	symbols := make([]string, 0, len(s.snapshots))
	for symbol := range s.snapshots {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	result := make([]LiveMarketEvent, 0, len(symbols))
	for _, symbol := range symbols {
		result = append(result, cloneLiveMarketEvent(s.snapshots[symbol]))
	}
	s.mu.RUnlock()
	return result
}

// Count returns the number of symbols currently represented in the store.
func (s *LiveMarketSnapshotStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.snapshots)
}

func normalizeSnapshotSymbol(providerSymbol string) string {
	return strings.ToUpper(strings.TrimSpace(providerSymbol))
}

// cloneLiveMarketEvent prevents the pgtype.Numeric.Int pointer from being
// shared between callers and the store. Numeric values are exact and mutable
// internally, so copying only the outer struct would not fully isolate them.
func cloneLiveMarketEvent(event LiveMarketEvent) LiveMarketEvent {
	event.Price = cloneLiveNumeric(event.Price)
	event.Quantity = cloneLiveNumeric(event.Quantity)
	return event
}

func cloneLiveNumeric(value pgtype.Numeric) pgtype.Numeric {
	clone := value
	if value.Int != nil {
		clone.Int = new(big.Int).Set(value.Int)
	}
	return clone
}
