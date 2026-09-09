package ingestion

import (
	// context identifies expected cancellation during API shutdown.
	"context"
	// errors lets the status store distinguish normal shutdown cancellation from
	// an unexpected monitor failure.
	"errors"
	// sync protects status reads from the monitor goroutine and API requests.
	"sync"
	// sort makes registry status output deterministic for logs, tests, and the
	// future multi-status API.
	"sort"
	// strings normalizes provider-aware registry keys.
	"strings"
	// time records when status changed and when the provider event was observed.
	"time"
)

const (
	// LiveMonitorState values are deliberately small and human-readable because
	// they are part of the operational API response.
	LiveMonitorStateDisabled     = "disabled"
	LiveMonitorStateStarting     = "starting"
	LiveMonitorStateRunning      = "running"
	LiveMonitorStateReconnecting = "reconnecting"
	LiveMonitorStateStopped      = "stopped"
	LiveMonitorStateError        = "error"
)

// LiveMonitorStatus describes the current lifecycle and counters for one
// optional live monitor. It is operational metadata, not a financial record.
type LiveMonitorStatus struct {
	Enabled                   bool
	Provider                  string
	CanonicalSymbol           string
	ProviderSymbol            string
	State                     string
	Received                  int64
	Accepted                  int64
	Rejected                  int64
	Reconnects                int64
	Persisted                 int64
	Restored                  int64
	LastEventObservedAt       *time.Time
	LastEventSourceReceivedAt *time.Time
	LastPersistedAt           *time.Time
	LastError                 string
	LastReconnectError        string
	LastPersistenceError      string
	UpdatedAt                 time.Time
}

// RecordReconnect counts a failed connection attempt that will be retried and
// makes the temporary reconnect state visible to API clients. A reconnect is
// not a terminal error because the stream may recover on the next attempt.
func (s *LiveMonitorStatusStore) RecordReconnect(reconnectErrors ...error) {
	s.mu.Lock()
	s.status.Reconnects++
	s.status.State = LiveMonitorStateReconnecting
	if len(reconnectErrors) > 0 && reconnectErrors[0] != nil {
		s.status.LastReconnectError = reconnectErrors[0].Error()
	} else {
		s.status.LastReconnectError = "unknown reconnect error"
	}
	s.status.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// RecordPersistenceSuccess records a successful durable latest-snapshot write.
func (s *LiveMonitorStatusStore) RecordPersistenceSuccess() {
	s.mu.Lock()
	s.status.Persisted++
	persistedAt := time.Now().UTC()
	s.status.LastPersistedAt = &persistedAt
	s.status.LastPersistenceError = ""
	s.status.UpdatedAt = persistedAt
	s.mu.Unlock()
}

// RecordPersistenceError keeps database-write failures visible without marking
// the live Binance monitor as stopped. The in-memory stream can continue while
// operators repair PostgreSQL or the migration.
func (s *LiveMonitorStatusStore) RecordPersistenceError(err error) {
	s.mu.Lock()
	if err == nil {
		s.status.LastPersistenceError = "unknown persistence error"
	} else {
		s.status.LastPersistenceError = err.Error()
	}
	s.status.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// RecordRestored records how many durable snapshots seeded the in-memory store
// during API startup. This is startup metadata, not a new live event count.
func (s *LiveMonitorStatusStore) RecordRestored(count int) {
	s.mu.Lock()
	s.status.Restored = int64(count)
	s.status.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// LiveMonitorStatusStore keeps a thread-safe status snapshot for the API.
// Keeping status separate from LiveMarketSnapshotStore prevents operational
// counters from being confused with the latest market value.
type LiveMonitorStatusStore struct {
	mu     sync.RWMutex
	status LiveMonitorStatus
}

// NewLiveMonitorStatusStore creates a status store representing a disabled
// optional monitor. cmd/api changes it to starting when a symbol is configured.
func NewLiveMonitorStatusStore() *LiveMonitorStatusStore {
	return &LiveMonitorStatusStore{
		status: LiveMonitorStatus{
			State:     LiveMonitorStateDisabled,
			UpdatedAt: time.Now().UTC(),
		},
	}
}

// Configure records the provider and symbol before the background goroutine
// begins connecting. This makes startup visible even while the network dial is
// still in progress.
func (s *LiveMonitorStatusStore) Configure(provider, providerSymbol string) {
	s.mu.Lock()
	s.status.Enabled = true
	s.status.Provider = provider
	s.status.ProviderSymbol = providerSymbol
	s.status.State = LiveMonitorStateStarting
	s.status.LastError = ""
	s.status.LastReconnectError = ""
	s.status.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// ConfigureInstrument records the full provider-aware identity for a monitor.
// Configure remains available for the Phase 2 single-symbol callers.
func (s *LiveMonitorStatusStore) ConfigureInstrument(reference InstrumentReference) {
	s.Configure(string(reference.Provider), reference.ProviderSymbol)
	s.mu.Lock()
	s.status.CanonicalSymbol = reference.CanonicalSymbol
	s.mu.Unlock()
}

// MarkRunning records that the monitor has been created and is entering its
// receive loop. A running monitor may still have zero accepted events.
func (s *LiveMonitorStatusStore) MarkRunning() {
	s.mu.Lock()
	s.status.State = LiveMonitorStateRunning
	s.status.LastError = ""
	s.status.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// RecordAccepted updates the live counters that are available for each valid
// event. The final monitor result later supplies rejected and received totals.
func (s *LiveMonitorStatusStore) RecordAccepted(event LiveMarketEvent) {
	s.mu.Lock()
	s.status.Accepted++
	// The first accepted event after a retry proves that the replacement
	// connection is healthy again, so transition back to running.
	if s.status.State == LiveMonitorStateReconnecting {
		s.status.State = LiveMonitorStateRunning
	}
	// While the monitor is still running, only accepted callbacks have reached
	// this store. Keep received at least as large as accepted so the in-progress
	// API response never reports an impossible accepted > received combination.
	if s.status.Received < s.status.Accepted {
		s.status.Received = s.status.Accepted
	}
	observedAt := event.ObservedAt
	receivedAt := event.SourceReceivedAt
	s.status.LastEventObservedAt = &observedAt
	s.status.LastEventSourceReceivedAt = &receivedAt
	s.status.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// RecordResult publishes the monitor's final counters and lifecycle state.
// Normal context cancellation is a stopped monitor, while another error is an
// operational failure that should be visible to API clients and operators.
func (s *LiveMonitorStatusStore) RecordResult(result LiveMarketRunResult, runErr error) {
	s.mu.Lock()
	s.status.Received = result.Received
	s.status.Accepted = result.Accepted
	s.status.Rejected = result.Rejected
	if result.LastEvent != nil {
		observedAt := result.LastEvent.ObservedAt
		receivedAt := result.LastEvent.SourceReceivedAt
		s.status.LastEventObservedAt = &observedAt
		s.status.LastEventSourceReceivedAt = &receivedAt
	}
	s.status.State = LiveMonitorStateStopped
	s.status.LastError = ""
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		s.status.State = LiveMonitorStateError
		s.status.LastError = runErr.Error()
	}
	s.status.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// MarkError records setup failures that happen before LiveMarketMonitor.Run
// can produce a LiveMarketRunResult.
func (s *LiveMonitorStatusStore) MarkError(err error) {
	s.mu.Lock()
	s.status.State = LiveMonitorStateError
	if err == nil {
		s.status.LastError = "unknown live monitor error"
	} else {
		s.status.LastError = err.Error()
	}
	s.status.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
}

// Snapshot returns a copy safe for JSON conversion by the HTTP layer.
func (s *LiveMonitorStatusStore) Snapshot() LiveMonitorStatus {
	s.mu.RLock()
	status := s.status
	if s.status.LastEventObservedAt != nil {
		observedAt := *s.status.LastEventObservedAt
		status.LastEventObservedAt = &observedAt
	}
	if s.status.LastEventSourceReceivedAt != nil {
		receivedAt := *s.status.LastEventSourceReceivedAt
		status.LastEventSourceReceivedAt = &receivedAt
	}
	if s.status.LastPersistedAt != nil {
		persistedAt := *s.status.LastPersistedAt
		status.LastPersistedAt = &persistedAt
	}
	s.mu.RUnlock()
	return status
}

// LiveMonitorStatusRegistry keeps one independent status store per provider
// and provider symbol. A failing monitor therefore changes only its own
// counters and state instead of overwriting another instrument's status.
type LiveMonitorStatusRegistry struct {
	mu     sync.RWMutex
	stores map[LiveMonitorStatusKey]*LiveMonitorStatusStore
}

// LiveMonitorStatusKey is the stable lookup key for one live monitor.
type LiveMonitorStatusKey struct {
	Provider       string
	ProviderSymbol string
}

// NewLiveMonitorStatusRegistry creates an empty multi-monitor registry.
func NewLiveMonitorStatusRegistry() *LiveMonitorStatusRegistry {
	return &LiveMonitorStatusRegistry{
		stores: make(map[LiveMonitorStatusKey]*LiveMonitorStatusStore),
	}
}

// Register returns the existing status store for a key or creates a configured
// one. Registration is idempotent so startup code can safely prepare a legacy
// singular view and the multi-symbol monitor loop can use the same registry.
func (r *LiveMonitorStatusRegistry) Register(reference InstrumentReference) *LiveMonitorStatusStore {
	key := normalizeLiveMonitorStatusKey(reference.Provider, reference.ProviderSymbol)
	r.mu.Lock()
	defer r.mu.Unlock()
	if store, exists := r.stores[key]; exists {
		return store
	}
	store := NewLiveMonitorStatusStore()
	store.ConfigureInstrument(reference)
	r.stores[key] = store
	return store
}

// Get returns one status store by provider and provider symbol.
func (r *LiveMonitorStatusRegistry) Get(provider, providerSymbol string) (*LiveMonitorStatusStore, bool) {
	key := normalizeLiveMonitorStatusKey(ProviderID(provider), providerSymbol)
	r.mu.RLock()
	store, exists := r.stores[key]
	r.mu.RUnlock()
	return store, exists
}

// List returns independent status snapshots in deterministic provider/symbol
// order. It is ready for the plural status endpoint planned in Phase 3.8.
func (r *LiveMonitorStatusRegistry) List() []LiveMonitorStatus {
	r.mu.RLock()
	keys := make([]LiveMonitorStatusKey, 0, len(r.stores))
	for key := range r.stores {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].Provider == keys[right].Provider {
			return keys[left].ProviderSymbol < keys[right].ProviderSymbol
		}
		return keys[left].Provider < keys[right].Provider
	})
	result := make([]LiveMonitorStatus, 0, len(keys))
	for _, key := range keys {
		result = append(result, r.stores[key].Snapshot())
	}
	r.mu.RUnlock()
	return result
}

func normalizeLiveMonitorStatusKey(provider ProviderID, providerSymbol string) LiveMonitorStatusKey {
	return LiveMonitorStatusKey{
		Provider:       strings.ToLower(strings.TrimSpace(string(provider))),
		ProviderSymbol: strings.ToUpper(strings.TrimSpace(providerSymbol)),
	}
}
