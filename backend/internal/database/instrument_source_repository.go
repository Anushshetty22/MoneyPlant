package database

import (
	// context carries cancellation and deadlines into the generated query methods.
	"context"
	// fmt adds operation and provider details to returned errors.
	"fmt"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InstrumentSource represents one provider-specific mapping for a canonical instrument.
//
// Phase 3.3 update: this model was added to keep provider identities separate
// from canonical instruments. ProviderInstrumentID is a pointer because Yahoo
// Finance does not require the numeric/token identifier used by Angel One.
type InstrumentSource struct {
	ID                   int64
	InstrumentID         int64
	Provider             string
	ProviderSymbol       string
	ProviderInstrumentID *string
	IsAuthoritative      bool
	IsActive             bool
	Metadata             []byte
}

// InstrumentSourceRepository is the application-facing wrapper around generated
// provider-mapping queries.
//
// Phase 3.3 update: this wrapper was added so future ingestion and API code can
// work with simple application models instead of generated sqlc and pgtype values.
type InstrumentSourceRepository struct {
	queries *generated.Queries
}

// NewInstrumentSourceRepository creates a provider-mapping repository using the
// existing shared PostgreSQL pool. It does not create another pool or connection.
func NewInstrumentSourceRepository(pool *pgxpool.Pool) *InstrumentSourceRepository {
	return &InstrumentSourceRepository{
		queries: generated.New(pool),
	}
}

// Create inserts one provider mapping for an existing canonical instrument.
//
// The database foreign key validates that instrumentID exists. The partial
// unique indexes validate provider tokens and the one-authoritative-source rule.
// sqlc binds the values safely and scans the returned row for this wrapper.
func (r *InstrumentSourceRepository) Create(
	ctx context.Context,
	instrumentID int64,
	provider string,
	providerSymbol string,
	providerInstrumentID *string,
	isAuthoritative bool,
	metadata []byte,
) (InstrumentSource, error) {
	// pgtype.Text represents SQL NULL explicitly when a provider has no token.
	providerID := pgtype.Text{}
	if providerInstrumentID != nil {
		providerID = pgtype.Text{String: *providerInstrumentID, Valid: true}
	}

	// The generated method performs the INSERT and converts the database row into
	// a generated result type; this wrapper then converts that result for callers.
	row, err := r.queries.CreateInstrumentSource(ctx, generated.CreateInstrumentSourceParams{
		InstrumentID:         instrumentID,
		Provider:             provider,
		ProviderSymbol:       providerSymbol,
		ProviderInstrumentID: providerID,
		IsAuthoritative:      isAuthoritative,
		Metadata:             metadata,
	})
	if err != nil {
		return InstrumentSource{}, fmt.Errorf("create %s source %q: %w", provider, providerSymbol, err)
	}

	return instrumentSourceFromGenerated(
		row.ID,
		row.InstrumentID,
		row.Provider,
		row.ProviderSymbol,
		row.ProviderInstrumentID,
		row.IsAuthoritative,
		row.IsActive,
		row.Metadata,
	), nil
}

// ListByCanonicalSymbol returns all provider mappings for one canonical instrument.
//
// The generated query performs the join from instruments to instrument_sources,
// so callers can use a stable symbol such as SBIN without first looking up its ID.
func (r *InstrumentSourceRepository) ListByCanonicalSymbol(ctx context.Context, canonicalSymbol string) ([]InstrumentSource, error) {
	// sqlc handles the many-row loop and rows.Err check. The wrapper maps every
	// generated row into the application model before returning the slice.
	rows, err := r.queries.ListInstrumentSourcesByCanonicalSymbol(ctx, canonicalSymbol)
	if err != nil {
		return nil, fmt.Errorf("list sources for %q: %w", canonicalSymbol, err)
	}

	sources := make([]InstrumentSource, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, instrumentSourceFromGenerated(
			row.ID,
			row.InstrumentID,
			row.Provider,
			row.ProviderSymbol,
			row.ProviderInstrumentID,
			row.IsAuthoritative,
			row.IsActive,
			row.Metadata,
		))
	}

	return sources, nil
}

// GetAuthoritativeByCanonicalSymbol returns the active preferred source for an instrument.
//
// If no authoritative mapping exists, PostgreSQL returns no rows and sqlc returns
// pgx.ErrNoRows. The caller can use that error to decide whether a fallback source
// or a future provider-resolution step is required.
func (r *InstrumentSourceRepository) GetAuthoritativeByCanonicalSymbol(ctx context.Context, canonicalSymbol string) (InstrumentSource, error) {
	// The generated method performs the filtered join and one-row scan.
	row, err := r.queries.GetAuthoritativeInstrumentSource(ctx, canonicalSymbol)
	if err != nil {
		return InstrumentSource{}, fmt.Errorf("get authoritative source for %q: %w", canonicalSymbol, err)
	}

	return instrumentSourceFromGenerated(
		row.ID,
		row.InstrumentID,
		row.Provider,
		row.ProviderSymbol,
		row.ProviderInstrumentID,
		row.IsAuthoritative,
		row.IsActive,
		row.Metadata,
	), nil
}

// instrumentSourceFromGenerated converts one generated sqlc row into the
// application model and centralizes nullable provider-token handling.
func instrumentSourceFromGenerated(
	id int64,
	instrumentID int64,
	provider string,
	providerSymbol string,
	providerInstrumentID pgtype.Text,
	isAuthoritative bool,
	isActive bool,
	metadata []byte,
) InstrumentSource {
	var providerID *string
	if providerInstrumentID.Valid {
		value := providerInstrumentID.String
		providerID = &value
	}

	return InstrumentSource{
		ID:                   id,
		InstrumentID:         instrumentID,
		Provider:             provider,
		ProviderSymbol:       providerSymbol,
		ProviderInstrumentID: providerID,
		IsAuthoritative:      isAuthoritative,
		IsActive:             isActive,
		Metadata:             metadata,
	}
}
