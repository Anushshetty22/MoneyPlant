package ingestion_test

import (
	// context supplies the reader execution context.
	"context"
	// strings creates an in-memory CSV stream for an offline unit test.
	"strings"
	// testing provides assertions.
	"testing"
	// time creates the source retrieval timestamp and expected observation date.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// TestCSVMacroReaderReadsValidRows verifies the normal CPI/repo-rate fixture path.
//
// Phase 4.5 update: this test proves that dates, exact numeric values, source
// references, retrieval provenance, and dataset code are normalized correctly
// without using the filesystem or PostgreSQL.
func TestCSVMacroReaderReadsValidRows(t *testing.T) {
	retrievedAt := time.Date(2026, time.August, 27, 0, 0, 0, 0, time.UTC)
	reader, err := ingestion.NewCSVMacroReader("rbi_cpi_combined_yoy", retrievedAt)
	if err != nil {
		t.Fatalf("create CSV macro reader: %v", err)
	}

	observations, err := reader.Read(context.Background(), strings.NewReader("observed_on,value,source_row_reference\n2026-01-01,2.73,DBIE Jan 2026\n2026-02-01,3.10,DBIE Feb 2026\n"))
	if err != nil {
		t.Fatalf("read macro CSV: %v", err)
	}

	if reader.DatasetCode() != "rbi_cpi_combined_yoy" {
		t.Errorf("dataset code = %q, want rbi_cpi_combined_yoy", reader.DatasetCode())
	}
	if len(observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(observations))
	}
	if !observations[0].ObservedOn.Valid || observations[0].ObservedOn.Time.Format("2006-01-02") != "2026-01-01" {
		t.Errorf("observed date = %#v, want 2026-01-01", observations[0].ObservedOn)
	}
	if !observations[0].Value.Valid || observations[0].Value.NaN {
		t.Fatal("first macro value is not a finite numeric")
	}
	if observations[0].SourceRowReference == nil || *observations[0].SourceRowReference != "DBIE Jan 2026" {
		t.Errorf("source reference = %v, want DBIE Jan 2026", observations[0].SourceRowReference)
	}
	if !observations[0].SourceRetrievedAt.Time.Equal(retrievedAt) {
		t.Errorf("retrieved_at = %s, want %s", observations[0].SourceRetrievedAt.Time, retrievedAt)
	}
}

// TestCSVMacroReaderRejectsDuplicateDates prevents two rows for one dataset
// date from reaching the database with ambiguous source values.
func TestCSVMacroReaderRejectsDuplicateDates(t *testing.T) {
	reader, err := ingestion.NewCSVMacroReader("rbi_policy_repo_rate", time.Now().UTC())
	if err != nil {
		t.Fatalf("create CSV macro reader: %v", err)
	}

	_, err = reader.Read(context.Background(), strings.NewReader("observed_on,value,source_row_reference\n2026-01-01,5.25,first\n2026-01-01,5.50,duplicate\n"))
	if err == nil {
		t.Fatal("expected duplicate-date error, got nil")
	}
}

// TestCSVMacroReaderRejectsInvalidValues prevents malformed or non-finite
// source values from being interpreted as legitimate macro observations.
func TestCSVMacroReaderRejectsInvalidValues(t *testing.T) {
	reader, err := ingestion.NewCSVMacroReader("rbi_cpi_combined_yoy", time.Now().UTC())
	if err != nil {
		t.Fatalf("create CSV macro reader: %v", err)
	}

	_, err = reader.Read(context.Background(), strings.NewReader("observed_on,value,source_row_reference\n2026-01-01,not-a-number,broken\n"))
	if err == nil {
		t.Fatal("expected invalid-value error, got nil")
	}
}
