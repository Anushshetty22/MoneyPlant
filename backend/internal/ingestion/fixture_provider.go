package ingestion

import (
	// context allows the fixture provider to stop work when the caller cancels.
	"context"
	// fmt creates an error when the fixture provider has no configured name.
	"fmt"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
)

// FixtureMarketDataProvider returns in-memory candles for offline testing.
//
// Phase 3.4 update: this fake provider was added to prove that the ingestion
// pipeline depends on an interface rather than Binance, Yahoo Finance, or Angel
// One code. Its output has the same database.MarketCandleInput shape expected
// from a real adapter.
type FixtureMarketDataProvider struct {
	name    string
	candles []database.MarketCandleInput
}

// NewFixtureMarketDataProvider creates an offline provider from fixture candles.
// The slice is copied so callers cannot unexpectedly change the provider after
// construction by mutating their original slice.
func NewFixtureMarketDataProvider(name string, candles []database.MarketCandleInput) (*FixtureMarketDataProvider, error) {
	if name == "" {
		return nil, fmt.Errorf("fixture provider name cannot be empty")
	}

	copiedCandles := append([]database.MarketCandleInput(nil), candles...)
	return &FixtureMarketDataProvider{name: name, candles: copiedCandles}, nil
}

// ProviderName identifies this source in ingestion-run provenance.
func (p *FixtureMarketDataProvider) ProviderName() string {
	return p.name
}

// FetchHistoricalCandles filters fixture rows by interval and half-open time range.
//
// The filtering behavior intentionally matches the market repository query:
// rows are included when observed_at >= From and observed_at < To. This makes
// the fake provider useful for testing the same window boundaries as a real API.
func (p *FixtureMarketDataProvider) FetchHistoricalCandles(
	ctx context.Context,
	request HistoricalCandleRequest,
) ([]database.MarketCandleInput, error) {
	// Check cancellation before processing the fixture collection.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	filtered := make([]database.MarketCandleInput, 0, len(p.candles))
	for _, candle := range p.candles {
		// Check periodically inside the loop so a large fixture can stop promptly.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if request.Interval != "" && candle.Interval != request.Interval {
			continue
		}
		if request.From.Valid && candle.ObservedAt.Valid && candle.ObservedAt.Time.Before(request.From.Time) {
			continue
		}
		if request.To.Valid && candle.ObservedAt.Valid && !candle.ObservedAt.Time.Before(request.To.Time) {
			continue
		}

		filtered = append(filtered, candle)
	}

	return filtered, nil
}
