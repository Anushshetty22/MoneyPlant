package main

import (
	// context creates the cancellation used to simulate API shutdown.
	"context"
	// encoding/json lets the test compare exact decimal values without turning
	// them into float64 values.
	"encoding/json"
	// errors checks sentinel errors through the monitor's wrapped messages.
	"errors"
	// fmt creates deterministic fake persistence errors.
	"fmt"
	// io represents a clean finite stream ending.
	"io"
	// strings checks that operational errors remain visible to the status API.
	"strings"
	// testing defines the automated lifecycle scenarios.
	"testing"
	// sync waits for independent monitor goroutines to finish in the multi-symbol test.
	"sync"
	// time creates deterministic UTC event timestamps and short test deadlines.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestRunLiveMonitorSavesFinalEventOnCancellation proves the shutdown path
// promised by Phase 2.15. The first event is saved immediately, then the fake
// persistence callback cancels the monitor. The monitor records that event as
// its LastEvent, allowing runLiveMonitor to save it one final time with a
// fresh context after cancellation.
func TestRunLiveMonitorSavesFinalEventOnCancellation(t *testing.T) {
	event := runtimeTestEvent(t, "78533.7700000000", "0.001280000000000000")
	stream := &runtimeScriptedStream{
		events:        []ingestion.LiveMarketEvent{event},
		blockAfterEOF: true,
	}
	monitor, err := ingestion.NewLiveMarketMonitor(stream)
	if err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	statusStore := configuredRuntimeStatusStore()
	store := ingestion.NewLiveMarketSnapshotStore()
	var persisted []ingestion.LiveMarketEvent
	persist := func(_ context.Context, savedEvent ingestion.LiveMarketEvent) error {
		persisted = append(persisted, savedEvent)
		if len(persisted) == 1 {
			// This represents the API receiving Ctrl+C immediately after the
			// first durable write begins.
			cancel()
		}
		return nil
	}

	result, runErr := runLiveMonitor(
		ctx,
		monitor,
		store,
		statusStore,
		persist,
		time.Hour,
		time.Second,
	)

	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("run error = %v, want context cancellation", runErr)
	}
	if result.Accepted != 1 || result.LastEvent == nil {
		t.Fatalf("unexpected monitor result: %#v", result)
	}
	if len(persisted) != 2 {
		t.Fatalf("persistence calls = %d, want initial and final save", len(persisted))
	}
	if numericJSON(t, persisted[1].Price) != numericJSON(t, event.Price) {
		t.Fatalf("final price changed from %s to %s", numericJSON(t, event.Price), numericJSON(t, persisted[1].Price))
	}
	if numericJSON(t, persisted[1].Quantity) != numericJSON(t, event.Quantity) {
		t.Fatalf("final quantity changed from %s to %s", numericJSON(t, event.Quantity), numericJSON(t, persisted[1].Quantity))
	}
	status := statusStore.Snapshot()
	if status.State != ingestion.LiveMonitorStateStopped {
		t.Fatalf("state = %q, want %q", status.State, ingestion.LiveMonitorStateStopped)
	}
	if status.Persisted != 2 {
		t.Fatalf("persisted = %d, want 2", status.Persisted)
	}
	if !stream.closed {
		t.Fatal("monitor did not close the stream during shutdown")
	}
}

// TestRunLiveMonitorSeparatesPersistenceFailureFromStreamFailure verifies that
// a database save problem is reported as persistence metadata while the live
// stream can still finish normally. A provider failure follows in a separate
// subtest and must instead produce the monitor's error state.
func TestRunLiveMonitorSeparatesPersistenceFailureFromStreamFailure(t *testing.T) {
	event := runtimeTestEvent(t, "78533.7700000000", "0.001280000000000000")

	t.Run("persistence failure keeps stream state separate", func(t *testing.T) {
		monitor := runtimeMonitor(t, &runtimeScriptedStream{events: []ingestion.LiveMarketEvent{event}})
		statusStore := configuredRuntimeStatusStore()
		persistenceErr := fmt.Errorf("database unavailable")

		result, runErr := runLiveMonitor(
			context.Background(),
			monitor,
			ingestion.NewLiveMarketSnapshotStore(),
			statusStore,
			func(context.Context, ingestion.LiveMarketEvent) error { return persistenceErr },
			time.Hour,
			time.Second,
		)

		if runErr != nil {
			t.Fatalf("run error = %v, want clean finite-stream completion", runErr)
		}
		if result.Accepted != 1 {
			t.Fatalf("accepted = %d, want 1", result.Accepted)
		}
		status := statusStore.Snapshot()
		if status.State != ingestion.LiveMonitorStateStopped {
			t.Fatalf("state = %q, want %q", status.State, ingestion.LiveMonitorStateStopped)
		}
		if status.LastPersistenceError != persistenceErr.Error() {
			t.Fatalf("persistence error = %q, want %q", status.LastPersistenceError, persistenceErr)
		}
		if status.LastError != "" {
			t.Fatalf("stream error = %q, want empty", status.LastError)
		}
	})

	t.Run("stream failure becomes monitor error", func(t *testing.T) {
		providerErr := fmt.Errorf("Binance connection lost")
		monitor := runtimeMonitor(t, &runtimeScriptedStream{
			events:    []ingestion.LiveMarketEvent{event},
			streamErr: providerErr,
		})
		statusStore := configuredRuntimeStatusStore()

		result, runErr := runLiveMonitor(
			context.Background(),
			monitor,
			ingestion.NewLiveMarketSnapshotStore(),
			statusStore,
			nil,
			time.Hour,
			time.Second,
		)

		if runErr == nil {
			t.Fatal("run error = nil, want provider failure")
		}
		if !strings.Contains(runErr.Error(), providerErr.Error()) {
			t.Fatalf("run error = %v, want provider failure", runErr)
		}
		if result.Accepted != 1 {
			t.Fatalf("accepted = %d, want 1", result.Accepted)
		}
		status := statusStore.Snapshot()
		if status.State != ingestion.LiveMonitorStateError {
			t.Fatalf("state = %q, want %q", status.State, ingestion.LiveMonitorStateError)
		}
		if !strings.Contains(status.LastError, providerErr.Error()) {
			t.Fatalf("last error = %q, want provider failure", status.LastError)
		}
		if status.LastPersistenceError != "" {
			t.Fatalf("persistence error = %q, want empty", status.LastPersistenceError)
		}
	})
}

func TestRunIndependentLiveMonitorsKeepFailureScopedToOneSymbol(t *testing.T) {
	failingEvent := runtimeTestEvent(t, "100", "1")
	failingEvent.ProviderSymbol = "BTCUSDT"
	healthyEvent := runtimeTestEvent(t, "200", "2")
	healthyEvent.ProviderSymbol = "ETHUSDT"

	failingMonitor := runtimeMonitor(t, &runtimeScriptedStream{
		streamErr: errors.New("BTCUSDT connection failed"),
	})
	healthyMonitor := runtimeMonitor(t, &runtimeScriptedStream{
		events: []ingestion.LiveMarketEvent{healthyEvent},
	})
	failingStatus := ingestion.NewLiveMonitorStatusStore()
	failingStatus.Configure("binance", "BTCUSDT")
	healthyStatus := ingestion.NewLiveMonitorStatusStore()
	healthyStatus.Configure("binance", "ETHUSDT")
	store := ingestion.NewLiveMarketSnapshotStore()

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		_, _ = runLiveMonitor(context.Background(), failingMonitor, store, failingStatus, nil, time.Second, time.Second)
	}()
	go func() {
		defer waitGroup.Done()
		_, _ = runLiveMonitor(context.Background(), healthyMonitor, store, healthyStatus, nil, time.Second, time.Second)
	}()
	waitGroup.Wait()

	if failingStatus.Snapshot().State != ingestion.LiveMonitorStateError {
		t.Fatalf("failing status = %#v, want error", failingStatus.Snapshot())
	}
	healthySnapshot := healthyStatus.Snapshot()
	if healthySnapshot.State != ingestion.LiveMonitorStateStopped || healthySnapshot.Accepted != 1 {
		t.Fatalf("healthy status = %#v, want stopped with one accepted event", healthySnapshot)
	}
	if store.Count() != 1 {
		t.Fatalf("shared snapshot count = %d, want healthy symbol only", store.Count())
	}
}

// TestRunLiveMonitorRecoversThroughReconnectBeforeShutdown verifies that a
// temporary provider failure is recovered by the reconnecting stream and that
// the recovered event still reaches the normal lifecycle/persistence path.
func TestRunLiveMonitorRecoversThroughReconnectBeforeShutdown(t *testing.T) {
	event := runtimeTestEvent(t, "78533.7700000000", "0.001280000000000000")
	firstStream := &runtimeScriptedStream{streamErr: fmt.Errorf("temporary connection failure")}
	secondStream := &runtimeScriptedStream{events: []ingestion.LiveMarketEvent{event}, blockAfterEOF: true}
	provider := &runtimeScriptedProvider{streams: []ingestion.LiveMarketStream{firstStream, secondStream}}
	policy := ingestion.DefaultLiveReconnectPolicy()
	policy.MaxRetries = 1
	policy.InitialBackoff = time.Millisecond
	policy.MaxBackoff = time.Millisecond
	policy.Sleep = func(context.Context, time.Duration) error { return nil }
	statusStore := configuredRuntimeStatusStore()
	policy.OnRetry = func(error, time.Duration) {
		statusStore.RecordReconnect()
	}
	reconnectingStream, err := ingestion.NewReconnectingLiveMarketStream(
		provider,
		ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTCUSDT"},
		policy,
	)
	if err != nil {
		t.Fatalf("create reconnecting stream: %v", err)
	}
	monitor := runtimeMonitor(t, reconnectingStream)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var persistenceCalls int
	result, runErr := runLiveMonitor(
		ctx,
		monitor,
		ingestion.NewLiveMarketSnapshotStore(),
		statusStore,
		func(context.Context, ingestion.LiveMarketEvent) error {
			persistenceCalls++
			if persistenceCalls == 1 {
				cancel()
			}
			return nil
		},
		time.Hour,
		time.Second,
	)

	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("run error = %v, want context cancellation", runErr)
	}
	if result.Accepted != 1 || statusStore.Snapshot().Reconnects != 1 {
		t.Fatalf("unexpected recovery result: result=%#v status=%#v", result, statusStore.Snapshot())
	}
	if !firstStream.closed || !secondStream.closed {
		t.Fatal("reconnect lifecycle did not close both provider streams")
	}
}

// TestRestoreDurableLiveSnapshots proves that restart recovery preserves the
// exact event data while converting the database representation back into the
// provider-neutral in-memory model.
func TestRestoreDurableLiveSnapshots(t *testing.T) {
	event := runtimeTestEvent(t, "78533.7700000000", "0.001280000000000000")
	repository := &runtimeSnapshotRepository{
		rows: []database.LiveMarketSnapshot{{
			ProviderSymbol:   event.ProviderSymbol,
			EventType:        event.EventType,
			ObservedAt:       pgtype.Timestamptz{Time: event.ObservedAt, Valid: true},
			Price:            event.Price,
			Quantity:         event.Quantity,
			SourceReceivedAt: pgtype.Timestamptz{Time: event.SourceReceivedAt, Valid: true},
		}},
	}
	store := ingestion.NewLiveMarketSnapshotStore()

	count := restoreDurableLiveSnapshots(context.Background(), repository, store)
	restored, ok := store.Get(event.ProviderSymbol)
	if count != 1 || !ok {
		t.Fatalf("restored count = %d, found = %v; want one snapshot", count, ok)
	}
	if numericJSON(t, restored.Price) != numericJSON(t, event.Price) {
		t.Fatalf("restored price = %s, want %s", numericJSON(t, restored.Price), numericJSON(t, event.Price))
	}
	if numericJSON(t, restored.Quantity) != numericJSON(t, event.Quantity) {
		t.Fatalf("restored quantity = %s, want %s", numericJSON(t, restored.Quantity), numericJSON(t, event.Quantity))
	}
	if !restored.ObservedAt.Equal(event.ObservedAt) || !restored.SourceReceivedAt.Equal(event.SourceReceivedAt) {
		t.Fatalf("restored timestamps changed: %#v", restored)
	}
}

type runtimeScriptedStream struct {
	events        []ingestion.LiveMarketEvent
	streamErr     error
	blockAfterEOF bool
	index         int
	closed        bool
}

type runtimeScriptedProvider struct {
	streams []ingestion.LiveMarketStream
	index   int
}

func (p *runtimeScriptedProvider) ProviderName() string { return "fixture" }

func (p *runtimeScriptedProvider) Capabilities() ingestion.ProviderCapabilities {
	return ingestion.ProviderCapabilities{Live: true}
}

func (p *runtimeScriptedProvider) OpenTradeStream(context.Context, ingestion.LiveMarketStreamRequest) (ingestion.LiveMarketStream, error) {
	if p.index >= len(p.streams) {
		return nil, fmt.Errorf("no scripted stream remains")
	}
	stream := p.streams[p.index]
	p.index++
	return stream, nil
}

func (s *runtimeScriptedStream) Receive(ctx context.Context) (ingestion.LiveMarketEvent, error) {
	if s.index < len(s.events) {
		event := s.events[s.index]
		s.index++
		return event, nil
	}
	if s.streamErr != nil {
		err := s.streamErr
		s.streamErr = nil
		return ingestion.LiveMarketEvent{}, err
	}
	if s.blockAfterEOF {
		<-ctx.Done()
		return ingestion.LiveMarketEvent{}, ctx.Err()
	}
	return ingestion.LiveMarketEvent{}, io.EOF
}

func (s *runtimeScriptedStream) Close() error {
	s.closed = true
	return nil
}

type runtimeSnapshotRepository struct {
	rows []database.LiveMarketSnapshot
}

func (r *runtimeSnapshotRepository) List(context.Context) ([]database.LiveMarketSnapshot, error) {
	return r.rows, nil
}

func (r *runtimeSnapshotRepository) Upsert(context.Context, database.LiveMarketSnapshotInput) (database.LiveMarketSnapshot, error) {
	return database.LiveMarketSnapshot{}, nil
}

func configuredRuntimeStatusStore() *ingestion.LiveMonitorStatusStore {
	statusStore := ingestion.NewLiveMonitorStatusStore()
	statusStore.Configure("binance", "BTCUSDT")
	return statusStore
}

func runtimeMonitor(t *testing.T, stream ingestion.LiveMarketStream) *ingestion.LiveMarketMonitor {
	t.Helper()
	monitor, err := ingestion.NewLiveMarketMonitor(stream)
	if err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	return monitor
}

func runtimeTestEvent(t *testing.T, price, quantity string) ingestion.LiveMarketEvent {
	t.Helper()
	observedAt := time.Date(2026, time.September, 8, 15, 2, 14, 0, time.UTC)
	return ingestion.LiveMarketEvent{
		ProviderSymbol:   "BTCUSDT",
		EventType:        "trade",
		ObservedAt:       observedAt,
		Price:            runtimeNumeric(t, price),
		Quantity:         runtimeNumeric(t, quantity),
		SourceReceivedAt: observedAt.Add(50 * time.Millisecond),
	}
}

func runtimeNumeric(t *testing.T, value string) pgtype.Numeric {
	t.Helper()
	var numeric pgtype.Numeric
	if err := numeric.Scan(value); err != nil {
		t.Fatalf("scan numeric %q: %v", value, err)
	}
	return numeric
}

func numericJSON(t *testing.T, value pgtype.Numeric) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal numeric: %v", err)
	}
	return string(encoded)
}
