package database

import (
	// context carries cancellation and deadlines into generated database queries.
	"context"
	// fmt adds operation details to returned errors.
	"fmt"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MarketCandleInput contains values required to insert one normalized candle.
//
// Phase 3.3 update: this input model was added so ingestion adapters can pass
// one named object rather than a long list of unrelated function arguments.
// Nullable fields represent provider data that may not exist for Yahoo Finance
// or Angel One but is available from Binance.
type MarketCandleInput struct {
	InstrumentSourceID  int64
	Interval            string
	ObservedAt          pgtype.Timestamptz
	SourceCloseAt       pgtype.Timestamptz
	Open                pgtype.Numeric
	High                pgtype.Numeric
	Low                 pgtype.Numeric
	Close               pgtype.Numeric
	Volume              pgtype.Numeric
	QuoteVolume         pgtype.Numeric
	TradeCount          pgtype.Int8
	TakerBuyVolume      pgtype.Numeric
	TakerBuyQuoteVolume pgtype.Numeric
	SourceRetrievedAt   pgtype.Timestamptz
}

// MarketCandle represents one normalized OHLCV observation returned by the repository.
//
// Numeric and timestamp fields remain pgtype values so the application does not
// lose decimal precision or timezone/NULL information before validation and API
// serialization are designed in later phases.
type MarketCandle struct {
	ID                  int64
	InstrumentSourceID  int64
	Interval            string
	ObservedAt          pgtype.Timestamptz
	SourceCloseAt       pgtype.Timestamptz
	Open                pgtype.Numeric
	High                pgtype.Numeric
	Low                 pgtype.Numeric
	Close               pgtype.Numeric
	Volume              pgtype.Numeric
	QuoteVolume         pgtype.Numeric
	TradeCount          pgtype.Int8
	TakerBuyVolume      pgtype.Numeric
	TakerBuyQuoteVolume pgtype.Numeric
	SourceRetrievedAt   pgtype.Timestamptz
}

// MarketCandleRepository wraps generated market-candle queries.
//
// Phase 3.3 update: this repository was added to keep ingestion and API code
// independent from generated sqlc row types and raw SQL implementation details.
type MarketCandleRepository struct {
	queries *generated.Queries
}

// NewMarketCandleRepository creates a repository using the shared PostgreSQL pool.
func NewMarketCandleRepository(pool *pgxpool.Pool) *MarketCandleRepository {
	return &MarketCandleRepository{
		queries: generated.New(pool),
	}
}

// Create inserts one candle and returns the stored normalized record.
//
// The database performs the important structural validation: OHLCV relationships,
// non-negative values, valid intervals, timestamps, foreign keys, and duplicate
// prevention. The repository passes the values to sqlc, which safely binds them
// and scans PostgreSQL's RETURNING row.
func (r *MarketCandleRepository) Create(ctx context.Context, input MarketCandleInput) (MarketCandle, error) {
	// The input struct maps directly to the generated parameter struct. Keeping
	// this mapping visible makes it clear which application value reaches each
	// database column.
	row, err := r.queries.CreateMarketCandle(ctx, generated.CreateMarketCandleParams{
		InstrumentSourceID:  input.InstrumentSourceID,
		Interval:            input.Interval,
		ObservedAt:          input.ObservedAt,
		SourceCloseAt:       input.SourceCloseAt,
		Open:                input.Open,
		High:                input.High,
		Low:                 input.Low,
		Close:               input.Close,
		Volume:              input.Volume,
		QuoteVolume:         input.QuoteVolume,
		TradeCount:          input.TradeCount,
		TakerBuyVolume:      input.TakerBuyVolume,
		TakerBuyQuoteVolume: input.TakerBuyQuoteVolume,
		SourceRetrievedAt:   input.SourceRetrievedAt,
	})
	if err != nil {
		return MarketCandle{}, fmt.Errorf("create market candle for source %d: %w", input.InstrumentSourceID, err)
	}

	return marketCandleFromGenerated(
		row.ID,
		row.InstrumentSourceID,
		row.Interval,
		row.ObservedAt,
		row.SourceCloseAt,
		row.Open,
		row.High,
		row.Low,
		row.Close,
		row.Volume,
		row.QuoteVolume,
		row.TradeCount,
		row.TakerBuyVolume,
		row.TakerBuyQuoteVolume,
		row.SourceRetrievedAt,
	), nil
}

// ListByCanonicalSymbol returns candles for one provider and interval in a UTC time range.
//
// The end of the range is exclusive. Therefore the query returns rows where
// observed_at >= from and observed_at < to. This makes adjacent ingestion or
// chart windows join cleanly without duplicating a boundary candle.
func (r *MarketCandleRepository) ListByCanonicalSymbol(
	ctx context.Context,
	canonicalSymbol string,
	provider string,
	interval string,
	from pgtype.Timestamptz,
	to pgtype.Timestamptz,
) ([]MarketCandle, error) {
	// The generated parameter names ObservedAt and ObservedAt_2 come from the
	// two positional timestamp parameters in the SQL query.
	rows, err := r.queries.ListMarketCandlesByCanonicalSymbol(ctx, generated.ListMarketCandlesByCanonicalSymbolParams{
		CanonicalSymbol: canonicalSymbol,
		Provider:        provider,
		Interval:        interval,
		ObservedAt:      from,
		ObservedAt_2:    to,
	})
	if err != nil {
		return nil, fmt.Errorf("list candles for %s via %s: %w", canonicalSymbol, provider, err)
	}

	// Convert each generated row into the application model while preserving the
	// database's exact numeric and nullable values.
	candles := make([]MarketCandle, 0, len(rows))
	for _, row := range rows {
		candles = append(candles, marketCandleFromGenerated(
			row.ID,
			row.InstrumentSourceID,
			row.Interval,
			row.ObservedAt,
			row.SourceCloseAt,
			row.Open,
			row.High,
			row.Low,
			row.Close,
			row.Volume,
			row.QuoteVolume,
			row.TradeCount,
			row.TakerBuyVolume,
			row.TakerBuyQuoteVolume,
			row.SourceRetrievedAt,
		))
	}

	return candles, nil
}

// marketCandleFromGenerated centralizes conversion from a generated sqlc row
// to the application model and keeps Create and List consistent.
func marketCandleFromGenerated(
	id int64,
	instrumentSourceID int64,
	interval string,
	observedAt pgtype.Timestamptz,
	sourceCloseAt pgtype.Timestamptz,
	open pgtype.Numeric,
	high pgtype.Numeric,
	low pgtype.Numeric,
	closeValue pgtype.Numeric,
	volume pgtype.Numeric,
	quoteVolume pgtype.Numeric,
	tradeCount pgtype.Int8,
	takerBuyVolume pgtype.Numeric,
	takerBuyQuoteVolume pgtype.Numeric,
	sourceRetrievedAt pgtype.Timestamptz,
) MarketCandle {
	return MarketCandle{
		ID:                  id,
		InstrumentSourceID:  instrumentSourceID,
		Interval:            interval,
		ObservedAt:          observedAt,
		SourceCloseAt:       sourceCloseAt,
		Open:                open,
		High:                high,
		Low:                 low,
		Close:               closeValue,
		Volume:              volume,
		QuoteVolume:         quoteVolume,
		TradeCount:          tradeCount,
		TakerBuyVolume:      takerBuyVolume,
		TakerBuyQuoteVolume: takerBuyQuoteVolume,
		SourceRetrievedAt:   sourceRetrievedAt,
	}
}
