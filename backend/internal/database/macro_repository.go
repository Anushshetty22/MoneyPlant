package database

import (
	// context carries cancellation and deadlines into generated database queries.
	"context"
	// fmt adds the operation and dataset code to returned errors.
	"fmt"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MacroDataset represents the definition of one macroeconomic time series.
//
// Phase 3.3 update: this model was added to keep CPI, repo-rate, and future
// dataset metadata separate from individual observations. BasePeriod is a
// pointer because not every macro series has an index base period.
type MacroDataset struct {
	ID              int64
	Code            string
	Name            string
	Provider        string
	Metric          string
	Unit            string
	Frequency       string
	ObservationType string
	BasePeriod      *string
	SourceURL       string
	RetrievedAt     pgtype.Timestamptz
	IsActive        bool
	Metadata        []byte
}

// MacroObservation represents one dated numeric value belonging to a dataset.
//
// ObservedOn remains pgtype.Date because the database intentionally stores a SQL
// DATE rather than inventing a time of day. Value remains pgtype.Numeric so the
// exact PostgreSQL numeric value is preserved instead of being converted to a
// binary floating-point number.
type MacroObservation struct {
	ID                 int64
	MacroDatasetID     int64
	ObservedOn         pgtype.Date
	Value              pgtype.Numeric
	SourceRetrievedAt  pgtype.Timestamptz
	SourceRowReference *string
	Metadata           []byte
}

// MacroDatasetRepository wraps generated dataset-definition queries.
//
// Phase 3.3 update: this wrapper hides generated sqlc row types from future
// ingestion commands and API handlers while preserving the database semantics.
type MacroDatasetRepository struct {
	queries *generated.Queries
}

// MacroObservationRepository wraps generated observation queries.
// It uses the same generated query executor and therefore the same shared pool.
type MacroObservationRepository struct {
	queries *generated.Queries
}

// NewMacroDatasetRepository creates a repository using the existing PostgreSQL pool.
func NewMacroDatasetRepository(pool *pgxpool.Pool) *MacroDatasetRepository {
	return &MacroDatasetRepository{
		queries: generated.New(pool),
	}
}

// NewMacroObservationRepository creates an observation repository using the same pool.
func NewMacroObservationRepository(pool *pgxpool.Pool) *MacroObservationRepository {
	return &MacroObservationRepository{
		queries: generated.New(pool),
	}
}

// Create inserts one macro dataset definition and returns the stored model.
func (r *MacroDatasetRepository) Create(
	ctx context.Context,
	code string,
	name string,
	provider string,
	metric string,
	unit string,
	frequency string,
	observationType string,
	basePeriod *string,
	sourceURL string,
	retrievedAt pgtype.Timestamptz,
	metadata []byte,
) (MacroDataset, error) {
	// Convert the optional base period into an explicit SQL NULL or text value.
	basePeriodValue := pgtype.Text{}
	if basePeriod != nil {
		basePeriodValue = pgtype.Text{String: *basePeriod, Valid: true}
	}

	// sqlc performs the INSERT and scans the RETURNING row into a generated type.
	row, err := r.queries.CreateMacroDataset(ctx, generated.CreateMacroDatasetParams{
		Code:            code,
		Name:            name,
		Provider:        provider,
		Metric:          metric,
		Unit:            unit,
		Frequency:       frequency,
		ObservationType: observationType,
		BasePeriod:      basePeriodValue,
		SourceUrl:       sourceURL,
		RetrievedAt:     retrievedAt,
		Metadata:        metadata,
	})
	if err != nil {
		return MacroDataset{}, fmt.Errorf("create macro dataset %q: %w", code, err)
	}

	return macroDatasetFromGenerated(
		row.ID,
		row.Code,
		row.Name,
		row.Provider,
		row.Metric,
		row.Unit,
		row.Frequency,
		row.ObservationType,
		row.BasePeriod,
		row.SourceUrl,
		row.RetrievedAt,
		row.IsActive,
		row.Metadata,
	), nil
}

// GetByCode retrieves one dataset definition using its stable code.
func (r *MacroDatasetRepository) GetByCode(ctx context.Context, code string) (MacroDataset, error) {
	// The generated method performs the unique-code lookup and row scan.
	row, err := r.queries.GetMacroDatasetByCode(ctx, code)
	if err != nil {
		return MacroDataset{}, fmt.Errorf("get macro dataset %q: %w", code, err)
	}

	return macroDatasetFromGenerated(
		row.ID,
		row.Code,
		row.Name,
		row.Provider,
		row.Metric,
		row.Unit,
		row.Frequency,
		row.ObservationType,
		row.BasePeriod,
		row.SourceUrl,
		row.RetrievedAt,
		row.IsActive,
		row.Metadata,
	), nil
}

// ListActive returns dataset definitions currently enabled for ingestion or display.
func (r *MacroDatasetRepository) ListActive(ctx context.Context) ([]MacroDataset, error) {
	// sqlc handles the many-row iteration; this wrapper maps each generated row.
	rows, err := r.queries.ListActiveMacroDatasets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active macro datasets: %w", err)
	}

	datasets := make([]MacroDataset, 0, len(rows))
	for _, row := range rows {
		datasets = append(datasets, macroDatasetFromGenerated(
			row.ID,
			row.Code,
			row.Name,
			row.Provider,
			row.Metric,
			row.Unit,
			row.Frequency,
			row.ObservationType,
			row.BasePeriod,
			row.SourceUrl,
			row.RetrievedAt,
			row.IsActive,
			row.Metadata,
		))
	}

	return datasets, nil
}

// Create inserts one dated numeric macro observation.
func (r *MacroObservationRepository) Create(
	ctx context.Context,
	macroDatasetID int64,
	observedOn pgtype.Date,
	value pgtype.Numeric,
	sourceRetrievedAt pgtype.Timestamptz,
	sourceRowReference *string,
	metadata []byte,
) (MacroObservation, error) {
	// Convert the optional source reference into SQL NULL or text.
	sourceReferenceValue := pgtype.Text{}
	if sourceRowReference != nil {
		sourceReferenceValue = pgtype.Text{String: *sourceRowReference, Valid: true}
	}

	// sqlc binds the exact date and numeric values and scans the returned row.
	row, err := r.queries.CreateMacroObservation(ctx, generated.CreateMacroObservationParams{
		MacroDatasetID:     macroDatasetID,
		ObservedOn:         observedOn,
		Value:              value,
		SourceRetrievedAt:  sourceRetrievedAt,
		SourceRowReference: sourceReferenceValue,
		Metadata:           metadata,
	})
	if err != nil {
		return MacroObservation{}, fmt.Errorf("create macro observation for dataset %d: %w", macroDatasetID, err)
	}

	return macroObservationFromGenerated(
		row.ID,
		row.MacroDatasetID,
		row.ObservedOn,
		row.Value,
		row.SourceRetrievedAt,
		row.SourceRowReference,
		row.Metadata,
	), nil
}

// ListByDatasetCode returns observations in chronological order for one dataset.
func (r *MacroObservationRepository) ListByDatasetCode(ctx context.Context, code string) ([]MacroObservation, error) {
	// The generated query joins observations to macro_datasets by code and returns
	// rows ordered by observed_on, so callers receive a ready-to-chart time series.
	rows, err := r.queries.ListMacroObservationsByDatasetCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("list observations for dataset %q: %w", code, err)
	}

	observations := make([]MacroObservation, 0, len(rows))
	for _, row := range rows {
		observations = append(observations, macroObservationFromGenerated(
			row.ID,
			row.MacroDatasetID,
			row.ObservedOn,
			row.Value,
			row.SourceRetrievedAt,
			row.SourceRowReference,
			row.Metadata,
		))
	}

	return observations, nil
}

// macroDatasetFromGenerated centralizes nullable base-period conversion.
func macroDatasetFromGenerated(
	id int64,
	code string,
	name string,
	provider string,
	metric string,
	unit string,
	frequency string,
	observationType string,
	basePeriod pgtype.Text,
	sourceURL string,
	retrievedAt pgtype.Timestamptz,
	isActive bool,
	metadata []byte,
) MacroDataset {
	var basePeriodValue *string
	if basePeriod.Valid {
		value := basePeriod.String
		basePeriodValue = &value
	}

	return MacroDataset{
		ID:              id,
		Code:            code,
		Name:            name,
		Provider:        provider,
		Metric:          metric,
		Unit:            unit,
		Frequency:       frequency,
		ObservationType: observationType,
		BasePeriod:      basePeriodValue,
		SourceURL:       sourceURL,
		RetrievedAt:     retrievedAt,
		IsActive:        isActive,
		Metadata:        metadata,
	}
}

// macroObservationFromGenerated centralizes nullable source-reference conversion.
func macroObservationFromGenerated(
	id int64,
	macroDatasetID int64,
	observedOn pgtype.Date,
	value pgtype.Numeric,
	sourceRetrievedAt pgtype.Timestamptz,
	sourceRowReference pgtype.Text,
	metadata []byte,
) MacroObservation {
	var sourceReferenceValue *string
	if sourceRowReference.Valid {
		value := sourceRowReference.String
		sourceReferenceValue = &value
	}

	return MacroObservation{
		ID:                 id,
		MacroDatasetID:     macroDatasetID,
		ObservedOn:         observedOn,
		Value:              value,
		SourceRetrievedAt:  sourceRetrievedAt,
		SourceRowReference: sourceReferenceValue,
		Metadata:           metadata,
	}
}
