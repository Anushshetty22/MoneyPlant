package main

import (
	// context carries cancellation into both the monitor and the persistence
	// callback. The final save intentionally receives a fresh bounded context.
	"context"
	// errors identifies normal shutdown so it is not logged as an operational
	// failure.
	"errors"
	// log records persistence failures without stopping a healthy live stream.
	"log"
	// time provides the persistence throttle and bounded final-save deadline.
	"time"
	// strings normalizes provider symbols used by the multi-provider status sink.
	"strings"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// liveSnapshotPersistence is the small boundary between live-stream lifecycle
// code and PostgreSQL. Tests can provide a deterministic function here, while
// the production API uses persistLiveSnapshot below.
type liveSnapshotPersistence func(context.Context, ingestion.LiveMarketEvent) error

type liveCandleRuntime struct {
	aggregator *ingestion.LiveMinuteCandleAggregator
	sourceID   func(ingestion.LiveMarketEvent) (int64, error)
	persist    func(context.Context, database.MarketCandleInput) error
	updates    liveUpdatePublisher
	statusFor  func(ingestion.LiveMarketEvent) ingestion.LiveMonitorStatus
}

type liveUpdatePublisher interface {
	PublishSnapshot(ingestion.LiveMarketEvent)
	PublishStatus(ingestion.LiveMonitorStatus)
}

// liveMonitorStatusSink keeps monitor execution independent from one or many
// public status records. Binance uses one store per stream; Angel One uses one
// multiplexed stream with one status store per subscribed provider symbol.
type liveMonitorStatusSink interface {
	MarkRunning()
	RecordAccepted(ingestion.LiveMarketEvent)
	RecordPersistenceSuccess()
	RecordPersistenceError(error)
	RecordResult(ingestion.LiveMarketRunResult, error)
}

// liveSnapshotRepository keeps the API orchestration independent from the
// concrete PostgreSQL repository. The production repository satisfies this
// interface, and lifecycle tests can use an in-memory fake.
type liveSnapshotRepository interface {
	List(context.Context) ([]database.LiveMarketSnapshot, error)
	Upsert(context.Context, database.LiveMarketSnapshotInput) (database.LiveMarketSnapshot, error)
}

// runLiveMonitor owns one monitor lifetime. It keeps every event current in
// memory, periodically saves a restart-safe value, always attempts one final
// save, and publishes the resulting lifecycle state to the status store.
//
// The final save uses a fresh context because the API's live-monitor context is
// canceled during graceful shutdown. Without this separate short deadline,
// the last event could be lost before PostgreSQL receives it.
func runLiveMonitor(
	ctx context.Context,
	monitor *ingestion.LiveMarketMonitor,
	store *ingestion.LiveMarketSnapshotStore,
	statusStore liveMonitorStatusSink,
	persist liveSnapshotPersistence,
	persistenceInterval time.Duration,
	finalPersistenceTimeout time.Duration,
	candleRuntimes ...liveCandleRuntime,
) (ingestion.LiveMarketRunResult, error) {
	if monitor == nil {
		return ingestion.LiveMarketRunResult{}, errors.New("live monitor cannot be nil")
	}
	if store == nil {
		return ingestion.LiveMarketRunResult{}, errors.New("live snapshot store cannot be nil")
	}
	if statusStore == nil {
		return ingestion.LiveMarketRunResult{}, errors.New("live monitor status store cannot be nil")
	}
	if persistenceInterval <= 0 {
		return ingestion.LiveMarketRunResult{}, errors.New("live persistence interval must be positive")
	}
	if finalPersistenceTimeout <= 0 {
		return ingestion.LiveMarketRunResult{}, errors.New("live final persistence timeout must be positive")
	}
	if len(candleRuntimes) > 1 {
		return ingestion.LiveMarketRunResult{}, errors.New("only one live candle runtime is supported")
	}

	statusStore.MarkRunning()
	var lastPersistenceAttemptAt time.Time

	// persistLatest throttles normal writes. The in-memory store is updated for
	// every trade, while PostgreSQL receives at most one periodic write and one
	// final write per monitor lifetime.
	persistLatest := func(eventContext context.Context, event ingestion.LiveMarketEvent) {
		if persist == nil {
			return
		}
		if !lastPersistenceAttemptAt.IsZero() && time.Since(lastPersistenceAttemptAt) < persistenceInterval {
			return
		}
		lastPersistenceAttemptAt = time.Now()
		if err := persist(eventContext, event); err != nil {
			statusStore.RecordPersistenceError(err)
			log.Printf("persist live snapshot: %v", err)
			return
		}
		statusStore.RecordPersistenceSuccess()
	}

	handleEvent := func(eventContext context.Context, event ingestion.LiveMarketEvent) error {
		if err := store.Handle(eventContext, event); err != nil {
			return err
		}
		statusStore.RecordAccepted(event)
		for _, runtime := range candleRuntimes {
			if runtime.updates == nil {
				continue
			}
			runtime.updates.PublishSnapshot(event)
			if runtime.statusFor != nil {
				runtime.updates.PublishStatus(runtime.statusFor(event))
			}
		}
		persistLatest(eventContext, event)
		if len(candleRuntimes) == 1 {
			persistClosedLiveCandles(eventContext, candleRuntimes[0], event, statusStore)
		}
		return nil
	}

	result, runErr := monitor.Run(ctx, handleEvent)
	if len(candleRuntimes) == 1 {
		flushLiveCandles(candleRuntimes[0], statusStore, finalPersistenceTimeout)
	}
	if result.LastEvent != nil && persist != nil {
		finalContext, cancelFinalPersistence := context.WithTimeout(context.Background(), finalPersistenceTimeout)
		if err := persist(finalContext, *result.LastEvent); err != nil {
			statusStore.RecordPersistenceError(err)
			log.Printf("persist final live snapshot: %v", err)
		} else {
			statusStore.RecordPersistenceSuccess()
		}
		cancelFinalPersistence()
	}

	statusStore.RecordResult(result, runErr)
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, context.DeadlineExceeded) {
		log.Printf("live monitor stopped with error: %v", runErr)
	}
	return result, runErr
}

func persistClosedLiveCandles(
	ctx context.Context,
	runtime liveCandleRuntime,
	event ingestion.LiveMarketEvent,
	statusStore liveMonitorStatusSink,
) {
	if runtime.aggregator == nil || runtime.sourceID == nil || runtime.persist == nil {
		return
	}
	sourceID, err := runtime.sourceID(event)
	if err != nil {
		statusStore.RecordPersistenceError(err)
		log.Printf("resolve live candle source: %v", err)
		return
	}
	if _, err := runtime.aggregator.Add(event, sourceID); err != nil {
		statusStore.RecordPersistenceError(err)
		log.Printf("aggregate live candle: %v", err)
		return
	}
	for _, candle := range runtime.aggregator.FlushClosed(event.ObservedAt) {
		if err := runtime.persist(ctx, candle); err != nil {
			statusStore.RecordPersistenceError(err)
			log.Printf("persist closed live candle: %v", err)
			continue
		}
		runtime.aggregator.MarkPersisted(candle)
	}
}

func flushLiveCandles(
	runtime liveCandleRuntime,
	statusStore liveMonitorStatusSink,
	finalPersistenceTimeout time.Duration,
) {
	if runtime.aggregator == nil || runtime.persist == nil {
		return
	}
	finalContext, cancel := context.WithTimeout(context.Background(), finalPersistenceTimeout)
	defer cancel()
	for _, candle := range runtime.aggregator.Flush() {
		if err := runtime.persist(finalContext, candle); err != nil {
			statusStore.RecordPersistenceError(err)
			log.Printf("persist final live candle: %v", err)
			continue
		}
		runtime.aggregator.MarkPersisted(candle)
	}
}

// multiplexedLiveMonitorStatusSink fans lifecycle updates out to the six
// independent Angel One status records while routing event counters and
// persistence results to the matching provider symbol.
type multiplexedLiveMonitorStatusSink struct {
	stores         map[string]*ingestion.LiveMonitorStatusStore
	lastEventStore *ingestion.LiveMonitorStatusStore
}

func newMultiplexedLiveMonitorStatusSink(stores map[string]*ingestion.LiveMonitorStatusStore) *multiplexedLiveMonitorStatusSink {
	return &multiplexedLiveMonitorStatusSink{stores: stores}
}

func (s *multiplexedLiveMonitorStatusSink) MarkRunning() {
	for _, store := range s.stores {
		store.MarkRunning()
	}
}

func (s *multiplexedLiveMonitorStatusSink) RecordAccepted(event ingestion.LiveMarketEvent) {
	store := s.stores[strings.ToUpper(strings.TrimSpace(event.ProviderSymbol))]
	if store == nil {
		return
	}
	s.lastEventStore = store
	store.RecordAccepted(event)
}

func (s *multiplexedLiveMonitorStatusSink) RecordPersistenceSuccess() {
	if s.lastEventStore != nil {
		s.lastEventStore.RecordPersistenceSuccess()
	}
}

func (s *multiplexedLiveMonitorStatusSink) RecordPersistenceError(err error) {
	if s.lastEventStore != nil {
		s.lastEventStore.RecordPersistenceError(err)
	}
}

func (s *multiplexedLiveMonitorStatusSink) RecordResult(result ingestion.LiveMarketRunResult, runErr error) {
	for providerSymbol, store := range s.stores {
		perSymbol := result.ByProviderSymbol[strings.ToUpper(strings.TrimSpace(providerSymbol))]
		store.RecordResult(perSymbol, runErr)
	}
}

// restoreDurableLiveSnapshots seeds the fast in-memory store from PostgreSQL
// before the API begins serving requests. A missing Phase 2.12 migration is
// logged but does not prevent the live API from starting for local learning.
func restoreDurableLiveSnapshots(
	ctx context.Context,
	repository liveSnapshotRepository,
	store *ingestion.LiveMarketSnapshotStore,
) int {
	rows, err := repository.List(ctx)
	if err != nil {
		log.Printf("restore durable live snapshots: %v", err)
		return 0
	}
	restoredCount := 0
	for _, row := range rows {
		event := ingestion.LiveMarketEvent{
			Provider:         ingestion.ProviderID(row.Provider),
			ProviderSymbol:   row.ProviderSymbol,
			EventType:        row.EventType,
			ObservedAt:       row.ObservedAt.Time,
			Price:            row.Price,
			Quantity:         row.Quantity,
			SourceReceivedAt: row.SourceReceivedAt.Time,
		}
		if err := store.Handle(ctx, event); err != nil {
			log.Printf("restore durable live snapshot for %s: %v", row.ProviderSymbol, err)
			continue
		}
		restoredCount++
	}
	if len(rows) > 0 {
		log.Printf("restored %d durable live snapshot(s)", restoredCount)
	}
	return restoredCount
}

func persistLiveSnapshot(
	ctx context.Context,
	repository liveSnapshotRepository,
	event ingestion.LiveMarketEvent,
) error {
	provider := event.Provider
	if provider == "" {
		// Phase 2 events predate the provider field and represent the original
		// Binance monitor. Keep those events persistable during the migration.
		provider = ingestion.ProviderBinance
	}
	_, err := repository.Upsert(ctx, database.LiveMarketSnapshotInput{
		Provider:         string(provider),
		ProviderSymbol:   event.ProviderSymbol,
		EventType:        event.EventType,
		ObservedAt:       pgtype.Timestamptz{Time: event.ObservedAt, Valid: true},
		Price:            event.Price,
		Quantity:         event.Quantity,
		SourceReceivedAt: pgtype.Timestamptz{Time: event.SourceReceivedAt, Valid: true},
	})
	return err
}
