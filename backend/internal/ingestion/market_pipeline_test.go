package ingestion_test

import (
	// context is passed through the provider and repository interfaces.
	"context"
	// errors creates a deterministic fake persistence failure.
	"errors"
	// fmt builds the fake repository's natural-key identifier.
	"fmt"
	// testing provides test execution and assertions.
	"testing"
	// time creates deterministic candle and ingestion-run timestamps.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestMarketIngestionServiceSuccess verifies the complete offline success path.
//
// Phase 3.4 update: this test proves that a fixture provider and in-memory
// repositories can travel through the same pipeline contracts as real providers
// and PostgreSQL repositories.
func TestMarketIngestionServiceSuccess(t *testing.T) {
	baseTime := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	provider, err := ingestion.NewFixtureMarketDataProvider("fixture", []database.MarketCandleInput{
		fixtureCandle(baseTime),
		fixtureCandle(baseTime.Add(24 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("create fixture provider: %v", err)
	}

	candleStore := &fakeCandleRepository{}
	runTracker := &fakeIngestionRunTracker{}
	service := ingestion.NewMarketIngestionService(provider, candleStore, runTracker)

	result, err := service.IngestHistorical(
		context.Background(),
		"BTCUSDT",
		42,
		ingestion.HistoricalCandleRequest{
			ProviderSymbol: "BTCUSDT",
			Interval:       "1d",
			From:           timestamp(baseTime),
			To:             timestamp(baseTime.Add(48 * time.Hour)),
		},
	)
	if err != nil {
		t.Fatalf("ingest fixture candles: %v", err)
	}

	if result.RowsReceived != 2 || result.RowsInserted != 2 || result.RowsRejected != 0 {
		t.Fatalf("result = %#v, want two received, two inserted, zero rejected", result)
	}
	if len(candleStore.created) != 2 {
		t.Fatalf("stored candles = %d, want 2", len(candleStore.created))
	}
	if candleStore.created[0].InstrumentSourceID != 42 {
		t.Fatalf("instrument source ID = %d, want 42", candleStore.created[0].InstrumentSourceID)
	}
	if runTracker.completed.Status != "succeeded" {
		t.Fatalf("completed run status = %q, want succeeded", runTracker.completed.Status)
	}
}

// TestMarketIngestionServicePartialFailure verifies that a persistence error after
// one successful row produces a partial run with accurate counts.
func TestMarketIngestionServicePartialFailure(t *testing.T) {
	baseTime := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	provider, err := ingestion.NewFixtureMarketDataProvider("fixture", []database.MarketCandleInput{
		fixtureCandle(baseTime),
		fixtureCandle(baseTime.Add(24 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("create fixture provider: %v", err)
	}

	// Fail on the second Create call so the pipeline must preserve the first row
	// and complete the audit record as partial.
	candleStore := &fakeCandleRepository{failOnCall: 2}
	runTracker := &fakeIngestionRunTracker{}
	service := ingestion.NewMarketIngestionService(provider, candleStore, runTracker)

	result, err := service.IngestHistorical(
		context.Background(),
		"BTCUSDT",
		42,
		ingestion.HistoricalCandleRequest{
			ProviderSymbol: "BTCUSDT",
			Interval:       "1d",
			From:           timestamp(baseTime),
			To:             timestamp(baseTime.Add(48 * time.Hour)),
		},
	)
	if err == nil {
		t.Fatal("expected persistence error, got nil")
	}

	if result.RowsReceived != 2 || result.RowsInserted != 1 || result.RowsRejected != 1 {
		t.Fatalf("result = %#v, want two received, one inserted, one rejected", result)
	}
	if runTracker.completed.Status != "partial" {
		t.Fatalf("completed run status = %q, want partial", runTracker.completed.Status)
	}
	if runTracker.completed.RowsRejected != 1 {
		t.Fatalf("completed rejected rows = %d, want 1", runTracker.completed.RowsRejected)
	}
}

// TestMarketIngestionServiceCountsRepeatedCandlesAsUpdates verifies that a
// second historical run is represented as updates rather than new inserts.
//
// Phase 5.4 update: this test covers the pipeline side of market-candle
// idempotency and confirms the ingestion audit counters remain meaningful.
func TestMarketIngestionServiceCountsRepeatedCandlesAsUpdates(t *testing.T) {
	baseTime := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	candles := []database.MarketCandleInput{fixtureCandle(baseTime)}
	provider, err := ingestion.NewFixtureMarketDataProvider("fixture", candles)
	if err != nil {
		t.Fatalf("create fixture provider: %v", err)
	}

	candleStore := &fakeCandleRepository{}
	runTracker := &fakeIngestionRunTracker{}
	service := ingestion.NewMarketIngestionService(provider, candleStore, runTracker)
	request := ingestion.HistoricalCandleRequest{
		ProviderSymbol: "BTCUSDT",
		Interval:       "1d",
		From:           timestamp(baseTime),
		To:             timestamp(baseTime.Add(24 * time.Hour)),
	}

	first, err := service.IngestHistorical(context.Background(), "BTCUSDT", 42, request)
	if err != nil {
		t.Fatalf("first market ingestion: %v", err)
	}
	second, err := service.IngestHistorical(context.Background(), "BTCUSDT", 42, request)
	if err != nil {
		t.Fatalf("second market ingestion: %v", err)
	}
	if first.RowsInserted != 1 || first.RowsUpdated != 0 {
		t.Fatalf("first result = %#v, want one insert", first)
	}
	if second.RowsInserted != 0 || second.RowsUpdated != 1 {
		t.Fatalf("second result = %#v, want one update", second)
	}
}

// TestMarketIngestionServiceFinalizesAfterCancellation verifies that audit
// cleanup is still possible after a provider cancels the main operation.
//
// Phase 5.2 update: this test protects the operational guarantee that a failed
// or canceled batch does not remain indefinitely in status=running.
func TestMarketIngestionServiceFinalizesAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	provider, err := ingestion.NewFixtureMarketDataProvider("fixture", nil)
	if err != nil {
		t.Fatalf("create fixture provider: %v", err)
	}
	// Cancel before the call so the provider returns context.Canceled. The run
	// tracker still receives a fresh cleanup context from the pipeline helper.
	cancel()
	runTracker := &fakeIngestionRunTracker{rejectCanceledComplete: true}
	service := ingestion.NewMarketIngestionService(provider, &fakeCandleRepository{}, runTracker)

	result, err := service.IngestHistorical(ctx, "BTCUSDT", 42, ingestion.HistoricalCandleRequest{
		ProviderSymbol: "BTCUSDT",
		Interval:       "1d",
		From:           timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		To:             timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)),
	})
	if err == nil {
		t.Fatal("expected canceled ingestion error, got nil")
	}
	if runTracker.completed.Status != "failed" {
		t.Fatalf("completed run status = %q, want failed", runTracker.completed.Status)
	}
	if result.RunID == 0 {
		t.Fatal("expected a run ID for the canceled operation")
	}
}

// TestMarketIngestionServiceRejectsInvalidProviderCandle verifies that the
// application validation gate runs before the candle repository is called.
//
// Phase 5.3 update: this test covers the operational path for a malformed
// provider row: the row is rejected, no persistence call is made, and the audit
// run is completed as partial.
func TestMarketIngestionServiceRejectsInvalidProviderCandle(t *testing.T) {
	baseTime := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	invalid := fixtureCandle(baseTime)
	invalid.High = numeric("99")
	provider, err := ingestion.NewFixtureMarketDataProvider("fixture", []database.MarketCandleInput{invalid})
	if err != nil {
		t.Fatalf("create fixture provider: %v", err)
	}

	candleStore := &fakeCandleRepository{}
	runTracker := &fakeIngestionRunTracker{}
	service := ingestion.NewMarketIngestionService(provider, candleStore, runTracker)

	result, err := service.IngestHistorical(context.Background(), "BTCUSDT", 42, ingestion.HistoricalCandleRequest{
		ProviderSymbol: "BTCUSDT",
		Interval:       "1d",
		From:           timestamp(baseTime),
		To:             timestamp(baseTime.Add(24 * time.Hour)),
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if result.RowsReceived != 1 || result.RowsInserted != 0 || result.RowsRejected != 1 {
		t.Fatalf("result = %#v, want one received, zero inserted, one rejected", result)
	}
	if len(candleStore.created) != 0 {
		t.Fatalf("stored candles = %d, want 0", len(candleStore.created))
	}
	if runTracker.completed.Status != "partial" {
		t.Fatalf("completed run status = %q, want partial", runTracker.completed.Status)
	}
}

// fixtureCandle creates the smallest valid input needed to test the pipeline's
// provider filtering and instrument-source assignment behavior.
func fixtureCandle(observedAt time.Time) database.MarketCandleInput {
	// Phase 5.1 update: fixtures now include a valid OHLCV shape because the
	// pipeline validates rows before sending them to the repository.
	return database.MarketCandleInput{
		InstrumentSourceID: 1,
		Interval:           "1d",
		ObservedAt:         timestamp(observedAt),
		Open:               numeric("100"),
		High:               numeric("105"),
		Low:                numeric("95"),
		Close:              numeric("102"),
		Volume:             numeric("1000"),
		SourceRetrievedAt:  timestamp(observedAt.Add(time.Hour)),
	}
}

// numeric converts an exact decimal test value into the pgtype model used by
// the database layer without routing it through float64.
func numeric(value string) pgtype.Numeric {
	var numericValue pgtype.Numeric
	if err := numericValue.Scan(value); err != nil {
		panic(err)
	}
	return numericValue
}

// timestamp converts a time.Time into the pgtype representation used by the
// database layer while keeping the test values explicitly valid and UTC.
func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

type fakeCandleRepository struct {
	created    []database.MarketCandleInput
	stored     map[string]struct{}
	failOnCall int
	calls      int
}

func (f *fakeCandleRepository) Upsert(_ context.Context, input database.MarketCandleInput) (database.MarketCandleWrite, error) {
	f.calls++
	if f.failOnCall != 0 && f.calls == f.failOnCall {
		return database.MarketCandleWrite{}, errors.New("simulated candle persistence failure")
	}
	if f.stored == nil {
		f.stored = make(map[string]struct{})
	}
	key := fmt.Sprintf("%d:%s:%s", input.InstrumentSourceID, input.Interval, input.ObservedAt.Time.UTC().Format(time.RFC3339Nano))
	_, exists := f.stored[key]
	f.stored[key] = struct{}{}
	f.created = append(f.created, input)
	return database.MarketCandleWrite{
		Candle:   database.MarketCandle{ID: int64(len(f.created))},
		Inserted: !exists,
	}, nil
}

func (f *fakeCandleRepository) ListByCanonicalSymbol(context.Context, string, string, string, pgtype.Timestamptz, pgtype.Timestamptz) ([]database.MarketCandle, error) {
	return nil, nil
}

type fakeIngestionRunTracker struct {
	nextID                 int64
	created                database.IngestionRun
	completed              database.IngestionRun
	rejectCanceledComplete bool
}

func (f *fakeIngestionRunTracker) Create(_ context.Context, runType, provider string, startedAt, requestedFrom, requestedTo pgtype.Timestamptz, scope []byte) (database.IngestionRun, error) {
	f.nextID++
	f.created = database.IngestionRun{
		ID:            f.nextID,
		RunType:       runType,
		Provider:      provider,
		Status:        "running",
		StartedAt:     startedAt,
		RequestedFrom: requestedFrom,
		RequestedTo:   requestedTo,
		Scope:         scope,
	}
	return f.created, nil
}

func (f *fakeIngestionRunTracker) Complete(ctx context.Context, id int64, status string, completedAt pgtype.Timestamptz, rowsReceived, rowsInserted, rowsUpdated, rowsRejected int64, errorMessage *string) (database.IngestionRun, error) {
	if f.rejectCanceledComplete && ctx.Err() != nil {
		return database.IngestionRun{}, ctx.Err()
	}
	f.completed = f.created
	f.completed.ID = id
	f.completed.Status = status
	f.completed.CompletedAt = completedAt
	f.completed.RowsReceived = rowsReceived
	f.completed.RowsInserted = rowsInserted
	f.completed.RowsUpdated = rowsUpdated
	f.completed.RowsRejected = rowsRejected
	f.completed.ErrorMessage = errorMessage
	return f.completed, nil
}

func (f *fakeIngestionRunTracker) ListRecentByProvider(context.Context, string, int32) ([]database.IngestionRun, error) {
	return nil, nil
}
