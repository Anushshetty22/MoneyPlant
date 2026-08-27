package ingestion

import (
	// fmt creates errors that identify the invalid candle field and rule.
	"fmt"
	// math/big allows exact decimal comparisons without converting financial
	// values through binary floating-point numbers.
	"math/big"
	// time validates finite timestamps and compares source lifecycle times.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

// ValidateMarketCandle checks a normalized candle before database persistence.
//
// Phase 5.1 update: application-level validation was added as a first quality
// gate before PostgreSQL. The database constraints remain the final safety net,
// while this function gives provider adapters and tests a reusable, readable
// validation contract.
//
// The rules cover source identity, supported interval, finite timestamps,
// non-negative OHLCV values, and the relationships that make an OHLC candle
// mathematically consistent.
func ValidateMarketCandle(candle database.MarketCandleInput) error {
	if candle.InstrumentSourceID <= 0 {
		return fmt.Errorf("instrument source ID must be positive")
	}
	if !allowedMarketIntervals[candle.Interval] {
		return fmt.Errorf("interval %q is not supported", candle.Interval)
	}
	if err := validateFiniteTimestamp(candle.ObservedAt, "observed_at"); err != nil {
		return err
	}
	if candle.SourceCloseAt.Valid {
		if err := validateFiniteTimestamp(candle.SourceCloseAt, "source_close_at"); err != nil {
			return err
		}
		if candle.SourceCloseAt.Time.Before(candle.ObservedAt.Time) {
			return fmt.Errorf("source_close_at must not be before observed_at")
		}
	}
	if err := validateFiniteTimestamp(candle.SourceRetrievedAt, "source_retrieved_at"); err != nil {
		return err
	}
	if candle.SourceRetrievedAt.Time.Before(candle.ObservedAt.Time) {
		return fmt.Errorf("source_retrieved_at must not be before observed_at")
	}

	openValue, err := finiteNumeric(candle.Open, "open")
	if err != nil {
		return err
	}
	highValue, err := finiteNumeric(candle.High, "high")
	if err != nil {
		return err
	}
	lowValue, err := finiteNumeric(candle.Low, "low")
	if err != nil {
		return err
	}
	closeValue, err := finiteNumeric(candle.Close, "close")
	if err != nil {
		return err
	}
	if _, err := finiteNumeric(candle.Volume, "volume"); err != nil {
		return err
	}

	zero := new(big.Rat)
	if openValue.Cmp(zero) < 0 || highValue.Cmp(zero) < 0 || lowValue.Cmp(zero) < 0 || closeValue.Cmp(zero) < 0 {
		return fmt.Errorf("open, high, low, and close must be non-negative")
	}
	if highValue.Cmp(openValue) < 0 || highValue.Cmp(closeValue) < 0 {
		return fmt.Errorf("high must be greater than or equal to open and close")
	}
	if lowValue.Cmp(openValue) > 0 || lowValue.Cmp(closeValue) > 0 {
		return fmt.Errorf("low must be less than or equal to open and close")
	}
	if highValue.Cmp(lowValue) < 0 {
		return fmt.Errorf("high must be greater than or equal to low")
	}

	if err := validateOptionalNumeric(candle.QuoteVolume, "quote_volume"); err != nil {
		return err
	}
	if err := validateOptionalNumeric(candle.TakerBuyVolume, "taker_buy_volume"); err != nil {
		return err
	}
	if err := validateOptionalNumeric(candle.TakerBuyQuoteVolume, "taker_buy_quote_volume"); err != nil {
		return err
	}
	if candle.TradeCount.Valid && candle.TradeCount.Int64 < 0 {
		return fmt.Errorf("trade_count must be non-negative")
	}

	return nil
}

// allowedMarketIntervals mirrors the database interval constraint and keeps
// provider-specific validation consistent with PostgreSQL's accepted values.
var allowedMarketIntervals = map[string]bool{
	"1m":  true,
	"5m":  true,
	"15m": true,
	"30m": true,
	"1h":  true,
	"4h":  true,
	"1d":  true,
	"1w":  true,
}

// validateFiniteTimestamp rejects NULL and PostgreSQL infinity values before
// time comparisons or database insertion.
func validateFiniteTimestamp(value pgtype.Timestamptz, fieldName string) error {
	if !value.Valid {
		return fmt.Errorf("%s must be present", fieldName)
	}
	if value.InfinityModifier != pgtype.Finite {
		return fmt.Errorf("%s must be finite", fieldName)
	}
	if value.Time.Equal(time.Time{}) {
		return fmt.Errorf("%s must contain a real timestamp", fieldName)
	}
	return nil
}

// finiteNumeric converts pgtype.Numeric's integer-and-exponent representation
// into an exact rational number. This preserves decimal precision while checking
// ordering and non-negative rules.
func finiteNumeric(value pgtype.Numeric, fieldName string) (*big.Rat, error) {
	if !value.Valid {
		return nil, fmt.Errorf("%s must be present", fieldName)
	}
	if value.NaN || value.InfinityModifier != pgtype.Finite {
		return nil, fmt.Errorf("%s must be finite", fieldName)
	}

	numerator := new(big.Int)
	if value.Int != nil {
		numerator.Set(value.Int)
	}
	if value.Exp >= 0 {
		numerator.Mul(numerator, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(value.Exp)), nil))
		return new(big.Rat).SetInt(numerator), nil
	}

	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-value.Exp)), nil)
	return new(big.Rat).SetFrac(numerator, denominator), nil
}

// validateOptionalNumeric applies non-negative and finite checks only when a
// provider supplied an optional metric. Invalid optional values are rejected;
// absent values remain valid for providers such as Yahoo Finance.
func validateOptionalNumeric(value pgtype.Numeric, fieldName string) error {
	if !value.Valid {
		return nil
	}
	numericValue, err := finiteNumeric(value, fieldName)
	if err != nil {
		return err
	}
	if numericValue.Sign() < 0 {
		return fmt.Errorf("%s must be non-negative", fieldName)
	}
	return nil
}
