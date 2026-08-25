package database

import (
	// context carries cancellation and deadlines into generated database queries.
	"context"
	// fmt adds operation and run/provider details to returned errors.
	"fmt"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IngestionRun represents the audit record for one collection or seed attempt.
//
// Phase 3.3 update: this application model was added so ingestion commands can
// track lifecycle state without depending directly on generated sqlc row types.
// ErrorMessage is a pointer because successful runs normally have no error.
type IngestionRun struct {
	ID            int64
	RunType       string
	Provider      string
	Status        string
	StartedAt     pgtype.Timestamptz
	CompletedAt   pgtype.Timestamptz
	RequestedFrom pgtype.Timestamptz
	RequestedTo   pgtype.Timestamptz
	RowsReceived  int64
	RowsInserted  int64
	RowsUpdated   int64
	RowsRejected  int64
	ErrorMessage  *string
	Scope         []byte
	CreatedAt     pgtype.Timestamptz
}

// IngestionRunRepository wraps generated ingestion-run queries.
//
// Phase 3.3 update: this repository was added to centralize the ingestion audit
// lifecycle: create a running record, complete it, and inspect recent history.
type IngestionRunRepository struct {
	queries *generated.Queries
}

// NewIngestionRunRepository creates a repository using the shared PostgreSQL pool.
func NewIngestionRunRepository(pool *pgxpool.Pool) *IngestionRunRepository {
	return &IngestionRunRepository{
		queries: generated.New(pool),
	}
}

// Create starts a new ingestion run in the running state.
//
// The SQL query sets status to running. The returned ID is used later by
// Complete to update the same audit record after ingestion succeeds or fails.
func (r *IngestionRunRepository) Create(
	ctx context.Context,
	runType string,
	provider string,
	startedAt pgtype.Timestamptz,
	requestedFrom pgtype.Timestamptz,
	requestedTo pgtype.Timestamptz,
	scope []byte,
) (IngestionRun, error) {
	// sqlc binds the operation metadata, requested range, and JSON scope, then
	// scans PostgreSQL defaults and the generated ID into an IngestionRun row.
	row, err := r.queries.CreateIngestionRun(ctx, generated.CreateIngestionRunParams{
		RunType:       runType,
		Provider:      provider,
		StartedAt:     startedAt,
		RequestedFrom: requestedFrom,
		RequestedTo:   requestedTo,
		Scope:         scope,
	})
	if err != nil {
		return IngestionRun{}, fmt.Errorf("create %s ingestion run for %s: %w", runType, provider, err)
	}

	return ingestionRunFromGenerated(row), nil
}

// Complete closes a run with its final status, counts, completion timestamp, and error.
//
// The database constraint rejects an inconsistent state, such as a succeeded run
// without completed_at or a running run with completed_at already populated.
func (r *IngestionRunRepository) Complete(
	ctx context.Context,
	id int64,
	status string,
	completedAt pgtype.Timestamptz,
	rowsReceived int64,
	rowsInserted int64,
	rowsUpdated int64,
	rowsRejected int64,
	errorMessage *string,
) (IngestionRun, error) {
	// Convert the optional error into SQL NULL for successful runs or text for
	// failed/partial runs.
	errorValue := pgtype.Text{}
	if errorMessage != nil {
		errorValue = pgtype.Text{String: *errorMessage, Valid: true}
	}

	// sqlc updates the existing run identified by id and returns the final audit row.
	row, err := r.queries.CompleteIngestionRun(ctx, generated.CompleteIngestionRunParams{
		ID:           id,
		Status:       status,
		CompletedAt:  completedAt,
		RowsReceived: rowsReceived,
		RowsInserted: rowsInserted,
		RowsUpdated:  rowsUpdated,
		RowsRejected: rowsRejected,
		ErrorMessage: errorValue,
	})
	if err != nil {
		return IngestionRun{}, fmt.Errorf("complete ingestion run %d: %w", id, err)
	}

	return ingestionRunFromGenerated(row), nil
}

// ListRecentByProvider returns recent audit records for one provider.
func (r *IngestionRunRepository) ListRecentByProvider(ctx context.Context, provider string, limit int32) ([]IngestionRun, error) {
	// The generated query applies the provider filter, newest-first ordering, and
	// row limit in PostgreSQL before returning the result slice.
	rows, err := r.queries.ListRecentIngestionRunsByProvider(ctx, generated.ListRecentIngestionRunsByProviderParams{
		Provider: provider,
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list recent ingestion runs for %s: %w", provider, err)
	}

	runs := make([]IngestionRun, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, ingestionRunFromGenerated(row))
	}

	return runs, nil
}

// ingestionRunFromGenerated converts nullable generated error text into the
// application model while preserving all timestamps, counters, and JSON scope.
func ingestionRunFromGenerated(row generated.IngestionRun) IngestionRun {
	var errorMessage *string
	if row.ErrorMessage.Valid {
		value := row.ErrorMessage.String
		errorMessage = &value
	}

	return IngestionRun{
		ID:            row.ID,
		RunType:       row.RunType,
		Provider:      row.Provider,
		Status:        row.Status,
		StartedAt:     row.StartedAt,
		CompletedAt:   row.CompletedAt,
		RequestedFrom: row.RequestedFrom,
		RequestedTo:   row.RequestedTo,
		RowsReceived:  row.RowsReceived,
		RowsInserted:  row.RowsInserted,
		RowsUpdated:   row.RowsUpdated,
		RowsRejected:  row.RowsRejected,
		ErrorMessage:  errorMessage,
		Scope:         row.Scope,
		CreatedAt:     row.CreatedAt,
	}
}
