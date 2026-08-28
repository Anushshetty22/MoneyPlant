package ingestion

import (
	// context carries cancellation through CSV parsing, repository writes, and audit updates.
	"context"
	// encoding/json stores the dataset identity in the ingestion-run scope.
	"encoding/json"
	// fmt creates descriptive macro-pipeline errors.
	"fmt"
	// io.Reader is the source stream accepted by MacroCSVReader.
	"io"
	// time supplies UTC lifecycle timestamps and range calculations.
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// MacroIngestionService coordinates one CSV reader, dataset definition,
// observation repository, and ingestion-run tracker.
//
// Phase 4.5 update: this service was added to separate macro parsing from
// PostgreSQL persistence and to make the full seed flow testable with fakes.
type MacroIngestionService struct {
	reader           MacroCSVReader
	datasetStore     MacroDatasetRepository
	observationStore MacroObservationRepository
	runTracker       IngestionRunTracker
}

// NewMacroIngestionService wires a CSV reader to the repositories it needs.
// The reader can be a real CSVMacroReader or a fake implementation in tests.
func NewMacroIngestionService(
	reader MacroCSVReader,
	datasetStore MacroDatasetRepository,
	observationStore MacroObservationRepository,
	runTracker IngestionRunTracker,
) *MacroIngestionService {
	return &MacroIngestionService{
		reader:           reader,
		datasetStore:     datasetStore,
		observationStore: observationStore,
		runTracker:       runTracker,
	}
}

// MacroIngestionResult summarizes one macro CSV seed operation.
type MacroIngestionResult struct {
	RunID        int64
	RowsReceived int64
	RowsInserted int64
	RowsUpdated  int64
	RowsRejected int64
}

// Seed reads, validates, upserts, and audits one macro CSV stream.
//
// The flow is:
//  1. Resolve the dataset definition by the reader's stable code.
//  2. Create a running macro_seed audit row before parsing.
//  3. Parse the CSV into normalized observations.
//  4. Upsert each observation by dataset and date.
//  5. Complete the audit row with inserted, updated, or rejected counts.
//
// A parsing failure marks the run failed. A persistence failure after earlier
// rows succeeded marks it partial, preserving the exact operational history.
func (s *MacroIngestionService) Seed(ctx context.Context, source io.Reader) (MacroIngestionResult, error) {
	dataset, err := s.datasetStore.GetByCode(ctx, s.reader.DatasetCode())
	if err != nil {
		return MacroIngestionResult{}, fmt.Errorf("resolve macro dataset %q: %w", s.reader.DatasetCode(), err)
	}

	scope, err := json.Marshal(map[string]string{
		"dataset_code":  s.reader.DatasetCode(),
		"source_format": "csv",
	})
	if err != nil {
		return MacroIngestionResult{}, fmt.Errorf("marshal macro ingestion scope: %w", err)
	}

	run, err := s.runTracker.Create(
		ctx,
		"macro_seed",
		dataset.Provider,
		pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		pgtype.Timestamptz{},
		pgtype.Timestamptz{},
		scope,
	)
	if err != nil {
		return MacroIngestionResult{}, fmt.Errorf("start macro ingestion run: %w", err)
	}

	observations, err := s.reader.Read(ctx, source)
	if err != nil {
		errorMessage := err.Error()
		if _, completeErr := completeIngestionRun(ctx, s.runTracker, run.ID, "failed", completionTimestamp(), 0, 0, 0, 0, &errorMessage); completeErr != nil {
			return MacroIngestionResult{RunID: run.ID}, fmt.Errorf("read macro CSV: %v; complete failed run: %w", err, completeErr)
		}
		return MacroIngestionResult{RunID: run.ID}, fmt.Errorf("read macro CSV: %w", err)
	}

	result := MacroIngestionResult{RunID: run.ID, RowsReceived: int64(len(observations))}
	for _, observation := range observations {
		writeResult, err := s.observationStore.Upsert(
			ctx,
			dataset.ID,
			observation.ObservedOn,
			observation.Value,
			observation.SourceRetrievedAt,
			observation.SourceRowReference,
			observation.Metadata,
		)
		if err != nil {
			errorMessage := err.Error()
			if _, completeErr := completeIngestionRun(ctx, s.runTracker, run.ID, "partial", completionTimestamp(), result.RowsReceived, result.RowsInserted, result.RowsUpdated, result.RowsRejected+1, &errorMessage); completeErr != nil {
				return result, fmt.Errorf("persist macro observation: %v; complete partial run: %w", err, completeErr)
			}
			result.RowsRejected++
			return result, fmt.Errorf("persist macro observation: %w", err)
		}

		if writeResult.Inserted {
			result.RowsInserted++
		} else {
			result.RowsUpdated++
		}
	}

	if _, err := completeIngestionRun(ctx, s.runTracker, run.ID, "succeeded", completionTimestamp(), result.RowsReceived, result.RowsInserted, result.RowsUpdated, result.RowsRejected, nil); err != nil {
		return result, fmt.Errorf("complete successful macro ingestion run: %w", err)
	}

	return result, nil
}
