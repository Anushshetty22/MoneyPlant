// Package ingestion contains provider adapters and provider-independent batch workflows.
package ingestion

import (
	// context allows a large CSV read to stop when the caller cancels the operation.
	"context"
	// encoding/csv parses comma-separated source files according to CSV quoting rules.
	"encoding/csv"
	// fmt creates errors that identify the invalid CSV row or column.
	"fmt"
	// io.Reader keeps the reader independent from local files, HTTP responses, and tests.
	"io"
	// strings trims whitespace and identifies optional blank source references.
	"strings"
	// time parses the ISO date column and records one retrieval timestamp for the read.
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// macroCSVObservedOnColumn is the canonical date column expected in a seed file.
	macroCSVObservedOnColumn = "observed_on"
	// macroCSVValueColumn is the numeric macro observation column expected in a seed file.
	macroCSVValueColumn = "value"
	// macroCSVSourceReferenceColumn preserves a source row, page, or series note.
	macroCSVSourceReferenceColumn = "source_row_reference"
)

// CSVMacroReader parses the common MoneyPlant macro seed format.
//
// Phase 4.5 update: this reader was added so CPI and RBI repo-rate files use
// the same validation path. It produces MacroObservationInput values but does
// not write to PostgreSQL; persistence belongs to the macro ingestion pipeline.
type CSVMacroReader struct {
	datasetCode       string
	sourceRetrievedAt pgtype.Timestamptz
}

// NewCSVMacroReader creates a reader for one known macro dataset.
//
// datasetCode is carried by the reader interface so the later pipeline can
// resolve the matching macro_datasets row. sourceRetrievedAt records when the
// source file was obtained, which is different from each observation's date.
func NewCSVMacroReader(datasetCode string, sourceRetrievedAt time.Time) (*CSVMacroReader, error) {
	datasetCode = strings.TrimSpace(datasetCode)
	if datasetCode == "" {
		return nil, fmt.Errorf("macro dataset code cannot be empty")
	}
	if sourceRetrievedAt.IsZero() {
		return nil, fmt.Errorf("macro source retrieval time cannot be zero")
	}

	return &CSVMacroReader{
		datasetCode: datasetCode,
		sourceRetrievedAt: pgtype.Timestamptz{
			Time:  sourceRetrievedAt.UTC(),
			Valid: true,
		},
	}, nil
}

// DatasetCode identifies the macro_datasets definition for the parsed rows.
func (r *CSVMacroReader) DatasetCode() string {
	return r.datasetCode
}

// Read validates and normalizes a CSV stream into macro observation inputs.
//
// The expected header is:
//
//	observed_on,value,source_row_reference
//
// Dates use YYYY-MM-DD. Values remain pgtype.Numeric so CPI percentages and
// policy rates are not converted through float64. Blank source references are
// represented as SQL NULL later, while blank or invalid dates and values fail
// immediately so bad data cannot silently enter the warehouse.
func (r *CSVMacroReader) Read(ctx context.Context, source io.Reader) ([]MacroObservationInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	reader := csv.NewReader(source)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("macro CSV is empty")
		}
		return nil, fmt.Errorf("read macro CSV header: %w", err)
	}
	if err := validateMacroCSVHeader(header); err != nil {
		return nil, err
	}

	observations := make([]MacroObservationInput, 0)
	seenDates := make(map[string]struct{})
	for rowNumber := 2; ; rowNumber++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read macro CSV row %d: %w", rowNumber, err)
		}
		if len(record) != 3 {
			return nil, fmt.Errorf("macro CSV row %d has %d columns, want 3", rowNumber, len(record))
		}

		dateText := strings.TrimSpace(record[0])
		if dateText == "" {
			return nil, fmt.Errorf("macro CSV row %d has an empty observed_on value", rowNumber)
		}
		observedTime, err := time.Parse("2006-01-02", dateText)
		if err != nil {
			return nil, fmt.Errorf("macro CSV row %d has invalid observed_on %q: %w", rowNumber, dateText, err)
		}
		if _, exists := seenDates[dateText]; exists {
			return nil, fmt.Errorf("macro CSV row %d duplicates observed_on %q", rowNumber, dateText)
		}
		seenDates[dateText] = struct{}{}

		valueText := strings.TrimSpace(record[1])
		if valueText == "" {
			return nil, fmt.Errorf("macro CSV row %d has an empty value", rowNumber)
		}
		var value pgtype.Numeric
		if err := value.Scan(valueText); err != nil {
			return nil, fmt.Errorf("macro CSV row %d has invalid value %q: %w", rowNumber, valueText, err)
		}
		if value.NaN || value.InfinityModifier != pgtype.Finite {
			return nil, fmt.Errorf("macro CSV row %d value %q must be finite", rowNumber, valueText)
		}

		var sourceReference *string
		if reference := strings.TrimSpace(record[2]); reference != "" {
			sourceReference = &reference
		}

		observations = append(observations, MacroObservationInput{
			ObservedOn: pgtype.Date{
				Time:  observedTime,
				Valid: true,
			},
			Value:              value,
			SourceRetrievedAt:  r.sourceRetrievedAt,
			SourceRowReference: sourceReference,
			Metadata:           []byte(`{"source_format":"csv"}`),
		})
	}

	return observations, nil
}

// validateMacroCSVHeader makes the input contract explicit and prevents a
// column reorder from silently changing the meaning of the parsed values.
func validateMacroCSVHeader(header []string) error {
	if len(header) != 3 {
		return fmt.Errorf("macro CSV header has %d columns, want 3", len(header))
	}
	expected := []string{macroCSVObservedOnColumn, macroCSVValueColumn, macroCSVSourceReferenceColumn}
	for index, name := range expected {
		if strings.TrimSpace(header[index]) != name {
			return fmt.Errorf("macro CSV header column %d = %q, want %q", index+1, header[index], name)
		}
	}
	return nil
}

// Compile-time assertion documents that CSVMacroReader satisfies the shared
// reader contract before a database-writing pipeline is introduced.
var _ MacroCSVReader = (*CSVMacroReader)(nil)
