package ingestion

import (
	// context identifies expected cancellation during API shutdown.
	"context"
	// errors lets the status store distinguish normal shutdown cancellation from
	// an unexpected monitor failure.
	"errors"
	// sync protects status reads from the monitor goroutine and API requests.
	"sync"
	// time records when status changed and when the provider event was observed.
	"time"
)

const (
	// LiveMonitorState values are deliberately small and human-readable because
	// they are part of the operational API response.
	LiveMonitorStateDisabled = "disabled"
	LiveMonitorStateStarting = "starting"
	LiveMonitorStateRunning  = "running"
	LiveMonitorStateStopped  = "stopped"
	LiveMonitorStateError    = "error"
)

// LiveMonitorStatus describes the current lifecycle and counters for one
// optional live monitor. It is operational metadata, not a financial record.
type LiveMonitorStatus struct {
	Enabled                   bool
	Provider                  string
	ProviderSymbol            string
	State                     string
	Received                  int64
	Accepted                  int64
	Rejected                  int64
	Reconnects                int64
	Persisted                 int64
	LastEventObservedAt       *time.Time
	LastEventSourceReceivedAt *time.Time
	LastPersistedAt           *time.Time
	LastError                 string
	LastPersistenceError      string
	UpdatedAt                 time.Time
}

// RecordReconnect counts a failed connection attempt that will be retried.
// A reconnect is not itself a terminal error because the stream may recover.
func (s *LiveMonitorStatusStore) RecordReconnect() {
	s.mu.Lock()
	s.status.Reconnects++
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
	s.status.UpdatedAt = time.Now().UTC()
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
