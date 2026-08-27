package ingestion

import (
	// context carries cancellation and deadlines through the complete batch.
	"context"
	// encoding/json records the request scope in the ingestion audit row.
	"encoding/json"
	// fmt creates descriptive pipeline errors.
	"fmt"
	// time supplies UTC timestamps for ingestion-run lifecycle records.
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// MarketIngestionService coordinates one historical market-data batch.
//
// Phase 3.4 update: this service was added to connect provider, repository, and
// ingestion-run interfaces without importing a concrete Binance or SQL implementation.
type MarketIngestionService struct {
	provider    HistoricalMarketDataProvider
	candleStore MarketCandleRepository
	runTracker  IngestionRunTracker
}

// NewMarketIngestionService wires one provider to candle persistence and run tracking.
// The concrete objects can be real repositories or test doubles implementing the interfaces.
func NewMarketIngestionService(
	provider HistoricalMarketDataProvider,
	candleStore MarketCandleRepository,
	runTracker IngestionRunTracker,
) *MarketIngestionService {
	return &MarketIngestionService{
		provider:    provider,
		candleStore: candleStore,
		runTracker:  runTracker,
	}
}

// MarketIngestionResult summarizes the normalized batch result for the caller.
type MarketIngestionResult struct {
	RunID        int64
	RowsReceived int64
	RowsInserted int64
	RowsUpdated  int64
	RowsRejected int64
}

// IngestHistorical fetches candles from the configured provider and persists them.
//
// The execution flow is:
//  1. Create an ingestion run with status running.
//  2. Ask the provider for normalized fixture/API rows.
//  3. Add the resolved instrument-source ID to each row.
//  4. Upsert each candle through the repository interface.
//  5. Complete the ingestion run with counts and status.
//
// A provider failure completes the run as failed. A persistence failure after
// some rows succeed completes it as partial, preserving the operational history.
func (s *MarketIngestionService) IngestHistorical(
	ctx context.Context,
	canonicalSymbol string,
	instrumentSourceID int64,
	request HistoricalCandleRequest,
) (MarketIngestionResult, error) {
	startedAt := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	scope, err := json.Marshal(map[string]string{
		"canonical_symbol": canonicalSymbol,
		"provider_symbol":  request.ProviderSymbol,
		"interval":         request.Interval,
	})
	if err != nil {
		return MarketIngestionResult{}, fmt.Errorf("marshal ingestion scope: %w", err)
	}

	// Start the audit record before external work so a failed provider call is visible.
	run, err := s.runTracker.Create(
		ctx,
		"market_api",
		s.provider.ProviderName(),
		startedAt,
		request.From,
		request.To,
		scope,
	)
	if err != nil {
		return MarketIngestionResult{}, fmt.Errorf("start market ingestion run: %w", err)
	}

	// Complete the run as failed if fetching returns an error. The original error
	// is returned as well so the command/API layer can report it to the caller.
	candles, err := s.provider.FetchHistoricalCandles(ctx, request)
	if err != nil {
		errorMessage := err.Error()
		if _, completeErr := s.runTracker.Complete(ctx, run.ID, "failed", completionTimestamp(), 0, 0, 0, 0, &errorMessage); completeErr != nil {
			return MarketIngestionResult{}, fmt.Errorf("fetch candles: %v; complete failed run: %w", err, completeErr)
		}
		return MarketIngestionResult{RunID: run.ID}, fmt.Errorf("fetch historical candles: %w", err)
	}

	result := MarketIngestionResult{RunID: run.ID, RowsReceived: int64(len(candles))}
	for _, candle := range candles {
		// The provider supplies market values, while the pipeline supplies the
		// resolved database mapping that identifies the exact source used.
		candle.InstrumentSourceID = instrumentSourceID

		// Phase 5.1 update: validate normalized provider data before persistence.
		// This produces a provider-independent quality error and prevents invalid
		// rows from reaching the database repository.
		if err := ValidateMarketCandle(candle); err != nil {
			errorMessage := err.Error()
			if _, completeErr := s.runTracker.Complete(ctx, run.ID, "partial", completionTimestamp(), result.RowsReceived, result.RowsInserted, result.RowsUpdated, result.RowsRejected+1, &errorMessage); completeErr != nil {
				return result, fmt.Errorf("validate market candle: %v; complete partial run: %w", err, completeErr)
			}
			result.RowsRejected++
			return result, fmt.Errorf("validate market candle: %w", err)
		}

		if _, err := s.candleStore.Create(ctx, candle); err != nil {
			errorMessage := err.Error()
			if _, completeErr := s.runTracker.Complete(ctx, run.ID, "partial", completionTimestamp(), result.RowsReceived, result.RowsInserted, result.RowsUpdated, result.RowsRejected+1, &errorMessage); completeErr != nil {
				return result, fmt.Errorf("persist candle: %v; complete partial run: %w", err, completeErr)
			}
			result.RowsRejected++
			return result, fmt.Errorf("persist market candle: %w", err)
		}

		// The current repository Create method performs an insert. Upsert/update
		// counting will be refined when the ingestion repository gains explicit
		// upsert result reporting.
		result.RowsInserted++
	}

	// Mark the run successful after every candle has been persisted.
	if _, err := s.runTracker.Complete(ctx, run.ID, "succeeded", completionTimestamp(), result.RowsReceived, result.RowsInserted, result.RowsUpdated, result.RowsRejected, nil); err != nil {
		return result, fmt.Errorf("complete successful ingestion run: %w", err)
	}

	return result, nil
}

// completionTimestamp creates a valid UTC database timestamp for run completion.
func completionTimestamp() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}
