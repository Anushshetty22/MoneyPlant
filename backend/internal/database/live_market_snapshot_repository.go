package database

import (
	// context carries cancellation and deadlines into PostgreSQL operations.
	"context"
	// fmt adds operation details to repository errors.
	"fmt"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LiveMarketSnapshotInput contains the normalized values needed to persist one
// latest live event. It intentionally contains no database identity fields.
type LiveMarketSnapshotInput struct {
	Provider         string
	ProviderSymbol   string
	EventType        string
	ObservedAt       pgtype.Timestamptz
	Price            pgtype.Numeric
	Quantity         pgtype.Numeric
	SourceReceivedAt pgtype.Timestamptz
}

// LiveMarketSnapshot is the durable latest-value representation. It is
// separate from ingestion.LiveMarketEvent so the database package does not
// expose generated SQL types to the streaming layer.
type LiveMarketSnapshot struct {
	ID               int64
	Provider         string
	ProviderSymbol   string
	EventType        string
	ObservedAt       pgtype.Timestamptz
	Price            pgtype.Numeric
	Quantity         pgtype.Numeric
	SourceReceivedAt pgtype.Timestamptz
	CreatedAt        pgtype.Timestamptz
	UpdatedAt        pgtype.Timestamptz
}

// LiveMarketSnapshotRepository wraps the sqlc queries for durable latest live
// values. The unique provider/symbol key makes each write an idempotent upsert.
type LiveMarketSnapshotRepository struct {
	queries *generated.Queries
}

// NewLiveMarketSnapshotRepository creates a repository using the shared pool.
func NewLiveMarketSnapshotRepository(pool *pgxpool.Pool) *LiveMarketSnapshotRepository {
	return &LiveMarketSnapshotRepository{queries: generated.New(pool)}
}

// Upsert stores the newest event for one provider symbol and returns the row.
func (r *LiveMarketSnapshotRepository) Upsert(ctx context.Context, input LiveMarketSnapshotInput) (LiveMarketSnapshot, error) {
	row, err := r.queries.UpsertLiveMarketSnapshot(ctx, generated.UpsertLiveMarketSnapshotParams{
		Provider:         input.Provider,
		ProviderSymbol:   input.ProviderSymbol,
		EventType:        input.EventType,
		ObservedAt:       input.ObservedAt,
		Price:            input.Price,
		Quantity:         input.Quantity,
		SourceReceivedAt: input.SourceReceivedAt,
	})
	if err != nil {
		return LiveMarketSnapshot{}, fmt.Errorf("upsert live market snapshot for %s:%s: %w", input.Provider, input.ProviderSymbol, err)
	}
	return liveMarketSnapshotFromGenerated(row), nil
}

// List returns all durable latest snapshots in deterministic symbol order.
func (r *LiveMarketSnapshotRepository) List(ctx context.Context) ([]LiveMarketSnapshot, error) {
	rows, err := r.queries.ListLiveMarketSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list live market snapshots: %w", err)
	}

	result := make([]LiveMarketSnapshot, 0, len(rows))
	for _, row := range rows {
		result = append(result, liveMarketSnapshotFromGenerated(row))
	}
	return result, nil
}

func liveMarketSnapshotFromGenerated(row generated.LiveMarketSnapshot) LiveMarketSnapshot {
	return LiveMarketSnapshot{
		ID:               row.ID,
		Provider:         row.Provider,
		ProviderSymbol:   row.ProviderSymbol,
		EventType:        row.EventType,
		ObservedAt:       row.ObservedAt,
		Price:            row.Price,
		Quantity:         row.Quantity,
		SourceReceivedAt: row.SourceReceivedAt,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}
