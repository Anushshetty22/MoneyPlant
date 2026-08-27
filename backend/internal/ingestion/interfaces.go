// Package ingestion contains provider-independent contracts for loading data.
//
// Phase 3.4 update: these interfaces were added to separate ingestion workflow
// logic from Binance, Yahoo Finance, Angel One, CSV, and PostgreSQL details.
// A fake provider can implement the same contracts as a real provider, which
// allows pipeline testing without credentials or internet access.
package ingestion

import (
	// context carries cancellation and deadlines through provider and repository calls.
	"context"
	// io.Reader allows a macro reader to consume a file, fixture, or HTTP response body.
	"io"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

// HistoricalCandleRequest describes the time window and interval requested from a market provider.
// Providers translate this provider-independent request into their own API format.
type HistoricalCandleRequest struct {
	ProviderSymbol string
	Interval       string
	From           pgtype.Timestamptz
	To             pgtype.Timestamptz
}

// HistoricalMarketDataProvider is implemented by Binance, Yahoo Finance, Angel One,
// and test fixtures that return normalized candle inputs.
//
// The interface exposes a provider name for provenance and accepts a normalized
// request. The adapter owns authentication, pagination, rate limits, response
// parsing, and conversion into database.MarketCandleInput.
type HistoricalMarketDataProvider interface {
	ProviderName() string
	FetchHistoricalCandles(context.Context, HistoricalCandleRequest) ([]database.MarketCandleInput, error)
}

// MacroObservationInput contains one normalized macro observation produced by a CSV reader.
// The dataset ID is added later after the pipeline resolves the dataset definition by code.
type MacroObservationInput struct {
	ObservedOn         pgtype.Date
	Value              pgtype.Numeric
	SourceRetrievedAt  pgtype.Timestamptz
	SourceRowReference *string
	Metadata           []byte
}

// MacroCSVReader parses one macro source format into normalized observations.
//
// Using io.Reader keeps the reader independent of storage. The same implementation
// can read a local fixture, an opened CSV file, or an HTTP response body.
type MacroCSVReader interface {
	DatasetCode() string
	Read(context.Context, io.Reader) ([]MacroObservationInput, error)
}

// InstrumentRepository defines the instrument operations required by ingestion.
// The concrete database repository satisfies this interface without being imported
// by the pipeline through a concrete type.
type InstrumentRepository interface {
	Create(context.Context, string, string, string, *string, string, []byte) (database.Instrument, error)
	GetByCanonicalSymbol(context.Context, string) (database.Instrument, error)
}

// InstrumentSourceRepository defines provider-mapping operations required to resolve
// canonical instruments into provider symbols and optional provider tokens.
type InstrumentSourceRepository interface {
	Create(context.Context, int64, string, string, *string, bool, []byte) (database.InstrumentSource, error)
	ListByCanonicalSymbol(context.Context, string) ([]database.InstrumentSource, error)
	GetAuthoritativeByCanonicalSymbol(context.Context, string) (database.InstrumentSource, error)
}

// MarketCandleRepository defines the persistence operations used by market ingestion.
type MarketCandleRepository interface {
	Create(context.Context, database.MarketCandleInput) (database.MarketCandle, error)
	ListByCanonicalSymbol(context.Context, string, string, string, pgtype.Timestamptz, pgtype.Timestamptz) ([]database.MarketCandle, error)
}

// MacroDatasetRepository defines dataset-definition operations used by macro ingestion.
type MacroDatasetRepository interface {
	Create(context.Context, string, string, string, string, string, string, string, *string, string, pgtype.Timestamptz, []byte) (database.MacroDataset, error)
	GetByCode(context.Context, string) (database.MacroDataset, error)
	ListActive(context.Context) ([]database.MacroDataset, error)
}

// MacroObservationRepository defines persistence operations for normalized macro values.
// Phase 4.5 update: Upsert was added so reseeding refreshes an existing date
// instead of failing on the macro_observations unique constraint.
type MacroObservationRepository interface {
	Upsert(context.Context, int64, pgtype.Date, pgtype.Numeric, pgtype.Timestamptz, *string, []byte) (database.MacroObservationWrite, error)
	ListByDatasetCode(context.Context, string) ([]database.MacroObservation, error)
}

// IngestionRunTracker defines the audit lifecycle for a batch operation.
// A pipeline creates a running record, performs work, then completes the same record.
type IngestionRunTracker interface {
	Create(context.Context, string, string, pgtype.Timestamptz, pgtype.Timestamptz, pgtype.Timestamptz, []byte) (database.IngestionRun, error)
	Complete(context.Context, int64, string, pgtype.Timestamptz, int64, int64, int64, int64, *string) (database.IngestionRun, error)
	ListRecentByProvider(context.Context, string, int32) ([]database.IngestionRun, error)
}
