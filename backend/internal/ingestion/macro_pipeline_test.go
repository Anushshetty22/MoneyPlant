package ingestion_test

import (
	// context is passed through the complete macro seed flow.
	"context"
	// strings provides repeatable in-memory CSV streams.
	"strings"
	// testing provides assertions.
	"testing"
	// time creates the reader provenance and dataset timestamps.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestMacroIngestionServiceSeedsAndReseeds verifies both sides of the macro
// persistence contract: new dates are inserted and existing dates are updated.
//
// Phase 4.5 update: this test proves that a CSV reader, dataset lookup, upsert
// repository, and ingestion-run tracker work together without PostgreSQL.
func TestMacroIngestionServiceSeedsAndReseeds(t *testing.T) {
	retrievedAt := time.Date(2026, time.August, 27, 0, 0, 0, 0, time.UTC)
	reader, err := ingestion.NewCSVMacroReader("rbi_cpi_combined_yoy", retrievedAt)
	if err != nil {
		t.Fatalf("create CSV macro reader: %v", err)
	}

	datasetStore := &fakeMacroDatasetRepository{dataset: database.MacroDataset{
		ID:       10,
		Code:     "rbi_cpi_combined_yoy",
		Provider: "rbi",
	}}
	observationStore := &fakeMacroObservationRepository{stored: make(map[string]database.MacroObservation)}
	runTracker := &fakeIngestionRunTracker{}
	service := ingestion.NewMacroIngestionService(reader, datasetStore, observationStore, runTracker)

	csvText := "observed_on,value,source_row_reference\n2026-01-01,2.73,Jan source row\n2026-02-01,3.10,Feb source row\n"
	result, err := service.Seed(context.Background(), strings.NewReader(csvText))
	if err != nil {
		t.Fatalf("seed macro observations: %v", err)
	}
	if result.RowsReceived != 2 || result.RowsInserted != 2 || result.RowsUpdated != 0 || result.RowsRejected != 0 {
		t.Fatalf("first result = %#v, want two inserts", result)
	}
	if runTracker.completed.Status != "succeeded" {
		t.Fatalf("first run status = %q, want succeeded", runTracker.completed.Status)
	}

	// Run the same source again. The fake repository recognizes the existing
	// dataset/date keys and exercises the update path used by safe reseeding.
	result, err = service.Seed(context.Background(), strings.NewReader(csvText))
	if err != nil {
		t.Fatalf("reseed macro observations: %v", err)
	}
	if result.RowsReceived != 2 || result.RowsInserted != 0 || result.RowsUpdated != 2 || result.RowsRejected != 0 {
		t.Fatalf("second result = %#v, want two updates", result)
	}
}

type fakeMacroDatasetRepository struct {
	dataset database.MacroDataset
}

func (f *fakeMacroDatasetRepository) Create(context.Context, string, string, string, string, string, string, string, *string, string, pgtype.Timestamptz, []byte) (database.MacroDataset, error) {
	return f.dataset, nil
}

func (f *fakeMacroDatasetRepository) GetByCode(context.Context, string) (database.MacroDataset, error) {
	return f.dataset, nil
}

func (f *fakeMacroDatasetRepository) ListActive(context.Context) ([]database.MacroDataset, error) {
	return []database.MacroDataset{f.dataset}, nil
}

type fakeMacroObservationRepository struct {
	stored map[string]database.MacroObservation
}

func (f *fakeMacroObservationRepository) Upsert(_ context.Context, datasetID int64, observedOn pgtype.Date, value pgtype.Numeric, retrievedAt pgtype.Timestamptz, sourceReference *string, metadata []byte) (database.MacroObservationWrite, error) {
	key := observedOn.Time.Format("2006-01-02")
	_, exists := f.stored[key]
	f.stored[key] = database.MacroObservation{
		MacroDatasetID:     datasetID,
		ObservedOn:         observedOn,
		Value:              value,
		SourceRetrievedAt:  retrievedAt,
		SourceRowReference: sourceReference,
		Metadata:           metadata,
	}
	return database.MacroObservationWrite{Observation: f.stored[key], Inserted: !exists}, nil
}

func (f *fakeMacroObservationRepository) ListByDatasetCode(context.Context, string) ([]database.MacroObservation, error) {
	return nil, nil
}
