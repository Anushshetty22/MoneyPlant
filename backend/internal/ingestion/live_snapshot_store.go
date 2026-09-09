package ingestion

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
)

// LiveMarketSnapshotStore keeps the latest valid event for each provider and
// provider symbol pair. The provider is part of the key because two providers
// may use the same symbol for different instruments or markets.
type LiveMarketSnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[liveSnapshotKey]LiveMarketEvent
}

type liveSnapshotKey struct {
	provider       ProviderID
	providerSymbol string
}

// NewLiveMarketSnapshotStore creates an empty snapshot store.
func NewLiveMarketSnapshotStore() *LiveMarketSnapshotStore {
	return &LiveMarketSnapshotStore{
		snapshots: make(map[liveSnapshotKey]LiveMarketEvent),
	}
}

// Handle implements LiveMarketEventHandler and stores one valid event.
// The monitor already validates events, but validating again at this boundary
// protects the store if another caller uses it directly in the future.
func (s *LiveMarketSnapshotStore) Handle(_ context.Context, event LiveMarketEvent) error {
	if err := ValidateLiveMarketEvent(event); err != nil {
		return fmt.Errorf("store live market snapshot: %w", err)
	}

	key := newLiveSnapshotKey(event.Provider, event.ProviderSymbol)
	event.Provider = key.provider
	s.mu.Lock()
	s.snapshots[key] = cloneLiveMarketEvent(event)
	s.mu.Unlock()
	return nil
}

// Get returns the latest event for one provider symbol. It remains available
// for the Phase 2 singular endpoint; callers that know the provider should use
// GetByProvider to avoid ambiguity.
func (s *LiveMarketSnapshotStore) Get(providerSymbol string) (LiveMarketEvent, bool) {
	symbol := normalizeSnapshotSymbol(providerSymbol)
	s.mu.RLock()
	keys := make([]liveSnapshotKey, 0, len(s.snapshots))
	for key := range s.snapshots {
		if key.providerSymbol == symbol {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].provider == keys[right].provider {
			return keys[left].providerSymbol < keys[right].providerSymbol
		}
		return keys[left].provider < keys[right].provider
	})
	var event LiveMarketEvent
	var exists bool
	if len(keys) > 0 {
		event, exists = s.snapshots[keys[0]]
	}
	s.mu.RUnlock()
	if !exists {
		return LiveMarketEvent{}, false
	}
	return cloneLiveMarketEvent(event), true
}

// GetByProvider returns one unambiguous provider/symbol snapshot.
func (s *LiveMarketSnapshotStore) GetByProvider(provider, providerSymbol string) (LiveMarketEvent, bool) {
	key := newLiveSnapshotKey(ProviderID(provider), providerSymbol)
	s.mu.RLock()
	event, exists := s.snapshots[key]
	s.mu.RUnlock()
	if !exists {
		return LiveMarketEvent{}, false
	}
	return cloneLiveMarketEvent(event), true
}

// List returns one latest event per provider/symbol pair in deterministic
// provider-then-symbol order.
func (s *LiveMarketSnapshotStore) List() []LiveMarketEvent {
	s.mu.RLock()
	keys := make([]liveSnapshotKey, 0, len(s.snapshots))
	for key := range s.snapshots {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].provider == keys[right].provider {
			return keys[left].providerSymbol < keys[right].providerSymbol
		}
		return keys[left].provider < keys[right].provider
	})

	result := make([]LiveMarketEvent, 0, len(s.snapshots))
	for _, key := range keys {
		result = append(result, cloneLiveMarketEvent(s.snapshots[key]))
	}
	s.mu.RUnlock()
	return result
}

// Count returns the number of provider/symbol pairs currently represented.
func (s *LiveMarketSnapshotStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.snapshots)
}

func normalizeSnapshotSymbol(providerSymbol string) string {
	return strings.ToUpper(strings.TrimSpace(providerSymbol))
}

func newLiveSnapshotKey(provider ProviderID, providerSymbol string) liveSnapshotKey {
	provider = ProviderID(strings.ToLower(strings.TrimSpace(string(provider))))
	if provider == "" {
		provider = ProviderUnknown
	}
	return liveSnapshotKey{
		provider:       provider,
		providerSymbol: normalizeSnapshotSymbol(providerSymbol),
	}
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
