package ingestion_test

import (
	// testing provides assertions.
	"testing"
	// time creates valid UTC timestamps for the test candle.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestValidateMarketCandleAcceptsValidOHLCV verifies the normal quality-gate path.
//
// Phase 5.1 update: this test documents the minimum valid normalized candle
// required before a provider row may reach a repository.
func TestValidateMarketCandleAcceptsValidOHLCV(t *testing.T) {
	candle := validCandle()
	if err := ingestion.ValidateMarketCandle(candle); err != nil {
		t.Fatalf("validate valid candle: %v", err)
	}
}

// TestValidateMarketCandleRejectsInconsistentOHLCV verifies that a malformed
// high value is rejected before database persistence.
func TestValidateMarketCandleRejectsInconsistentOHLCV(t *testing.T) {
	candle := validCandle()
	candle.High = exactNumeric("99")

	if err := ingestion.ValidateMarketCandle(candle); err == nil {
		t.Fatal("expected high/open relationship error, got nil")
	}
}

// TestValidateMarketCandleRejectsMissingRequiredValues verifies that missing
// source identity and OHLCV values cannot be mistaken for zero.
func TestValidateMarketCandleRejectsMissingRequiredValues(t *testing.T) {
	candle := validCandle()
	candle.InstrumentSourceID = 0
	candle.Volume = pgtype.Numeric{}

	if err := ingestion.ValidateMarketCandle(candle); err == nil {
		t.Fatal("expected missing source identity error, got nil")
	}
}

func validCandle() database.MarketCandleInput {
	baseTime := time.Date(2026, time.August, 27, 0, 0, 0, 0, time.UTC)
	return database.MarketCandleInput{
		InstrumentSourceID: 1,
		Interval:           "1d",
		ObservedAt:         validationTimestamp(baseTime),
		Open:               exactNumeric("100"),
		High:               exactNumeric("105"),
		Low:                exactNumeric("95"),
		Close:              exactNumeric("102"),
		Volume:             exactNumeric("1000"),
		SourceRetrievedAt:  validationTimestamp(baseTime.Add(time.Hour)),
	}
}

func exactNumeric(value string) pgtype.Numeric {
	var numericValue pgtype.Numeric
	if err := numericValue.Scan(value); err != nil {
		panic(err)
	}
	return numericValue
}

func validationTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
