// Package analytics contains provider-independent calculations over normalized
// market candles. It does not know whether a candle came from Binance,
// Angel One, Yahoo Finance, or a fixture.
package analytics

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
)

const (
	shortMovingAverageWindow  = 7
	mediumMovingAverageWindow = 20
	longMovingAverageWindow   = 50
	volatilityWindow          = 20
	annualTradingPeriods      = 252
	outputDecimalPlaces       = 10
)

// Candle is the minimum normalized input needed for market analytics. Close is
// kept as decimal text so callers do not have to convert financial values
// through float64 before the exact calculation layer sees them.
type Candle struct {
	ObservedAt time.Time
	Close      string
}

// SeriesPoint contains one calculated observation. A nil metric means the
// requested rolling window has not warmed up yet; it is never represented as
// a fabricated zero.
type SeriesPoint struct {
	ObservedAt       time.Time
	Close            string
	PeriodReturn     *string
	CumulativeReturn *string
	SMA7             *string
	SMA20            *string
	SMA50            *string
	Volatility20     *string
	Drawdown         *string
}

// Summary contains range-level analytics for one instrument.
type Summary struct {
	CandleCount          int
	FirstObservedAt      time.Time
	LastObservedAt       time.Time
	FirstClose           string
	LastClose            string
	TotalReturn          *string
	MaximumDrawdown      *string
	AnnualizedVolatility *string
}

// Result is the complete analytics result for one ordered candle series.
type Result struct {
	Summary Summary
	Series  []SeriesPoint
}

type analyticsWindow struct {
	from time.Time
	to   time.Time
}

// ComparisonInput identifies one canonical instrument's candle series. The
// caller is responsible for resolving the requested provider mapping before
// passing candles into this provider-independent package.
type ComparisonInput struct {
	Symbol  string
	Candles []Candle
}

// ComparisonPoint is a normalized performance value where the first available
// close for that instrument is represented as 100.
type ComparisonPoint struct {
	ObservedAt      time.Time
	NormalizedClose string
}

// ComparisonSeries contains one normalized series and its total return.
type ComparisonSeries struct {
	Symbol      string
	FirstClose  string
	LastClose   string
	TotalReturn *string
	Series      []ComparisonPoint
}

// Calculate computes daily-style market analytics over the supplied candles.
// The function sorts a copy of the input by observation time, rejects duplicate
// timestamps and non-positive closes, and treats rolling windows as observation
// counts rather than calendar-day counts. Empty input is valid and returns an
// empty result.
func Calculate(candles []Candle) (Result, error) {
	return calculate(candles, nil)
}

// CalculateWindow computes indicators using all supplied candles but returns
// only the requested half-open time range. Earlier candles therefore warm up
// moving averages and volatility without leaking outside-range points into the
// API response or range-level summary.
func CalculateWindow(candles []Candle, from, to time.Time) (Result, error) {
	from = from.UTC()
	to = to.UTC()
	if !from.Before(to) {
		return Result{}, fmt.Errorf("analytics window must have to after from")
	}
	return calculate(candles, &analyticsWindow{from: from, to: to})
}

func calculate(candles []Candle, window *analyticsWindow) (Result, error) {
	normalized, closes, err := normalizeCandles(candles)
	if err != nil {
		return Result{}, err
	}
	if len(normalized) == 0 {
		return Result{Series: []SeriesPoint{}}, nil
	}

	periodReturns := make([]*big.Rat, len(normalized))
	series := make([]SeriesPoint, len(normalized))
	firstClose := closes[0]
	runningHigh := new(big.Rat).Set(firstClose)
	maximumDrawdown := new(big.Rat)

	for index, candle := range normalized {
		point := SeriesPoint{
			ObservedAt: candle.ObservedAt,
			Close:      candle.Close,
		}

		if index > 0 {
			periodReturn := subtractOne(divide(closes[index], closes[index-1]))
			periodReturns[index] = periodReturn
			point.PeriodReturn = stringPointer(formatRat(periodReturn))
		}

		point.CumulativeReturn = stringPointer(formatRat(subtractOne(divide(closes[index], firstClose))))

		if index+1 >= shortMovingAverageWindow {
			point.SMA7 = stringPointer(formatRat(average(closes[index+1-shortMovingAverageWindow : index+1])))
		}
		if index+1 >= mediumMovingAverageWindow {
			point.SMA20 = stringPointer(formatRat(average(closes[index+1-mediumMovingAverageWindow : index+1])))
		}
		if index+1 >= longMovingAverageWindow {
			point.SMA50 = stringPointer(formatRat(average(closes[index+1-longMovingAverageWindow : index+1])))
		}

		if index >= volatilityWindow {
			volatility := annualizedVolatility(periodReturns[index-volatilityWindow+1 : index+1])
			point.Volatility20 = stringPointer(formatFloat(volatility))
		}

		if closes[index].Cmp(runningHigh) > 0 {
			runningHigh.Set(closes[index])
		}
		drawdown := subtractOne(divide(closes[index], runningHigh))
		point.Drawdown = stringPointer(formatRat(drawdown))
		if drawdown.Cmp(maximumDrawdown) < 0 {
			maximumDrawdown.Set(drawdown)
		}
		series[index] = point
	}

	totalReturn := formatRat(subtractOne(divide(closes[len(closes)-1], firstClose)))
	result := Result{
		Summary: Summary{
			CandleCount:          len(normalized),
			FirstObservedAt:      normalized[0].ObservedAt,
			LastObservedAt:       normalized[len(normalized)-1].ObservedAt,
			FirstClose:           normalized[0].Close,
			LastClose:            normalized[len(normalized)-1].Close,
			TotalReturn:          stringPointer(totalReturn),
			MaximumDrawdown:      stringPointer(formatRat(maximumDrawdown)),
			AnnualizedVolatility: latestVolatility(series),
		},
		Series: series,
	}
	if window == nil {
		return result, nil
	}
	return applyWindow(result, normalized, closes, *window), nil
}

// Compare normalizes each non-empty instrument independently to a starting
// value of 100. Inputs must have unique non-empty symbols. Empty candle series
// are returned as empty comparison series so callers can report unavailable
// data without inventing values.
func Compare(inputs []ComparisonInput) ([]ComparisonSeries, error) {
	seen := make(map[string]struct{}, len(inputs))
	result := make([]ComparisonSeries, 0, len(inputs))
	for _, input := range inputs {
		symbol := strings.ToUpper(strings.TrimSpace(input.Symbol))
		if symbol == "" {
			return nil, fmt.Errorf("comparison symbol cannot be empty")
		}
		if _, exists := seen[symbol]; exists {
			return nil, fmt.Errorf("comparison symbol %q is duplicated", symbol)
		}
		seen[symbol] = struct{}{}

		normalized, closes, err := normalizeCandles(input.Candles)
		if err != nil {
			return nil, fmt.Errorf("normalize comparison %s: %w", symbol, err)
		}
		comparison := ComparisonSeries{Symbol: symbol, Series: []ComparisonPoint{}}
		if len(normalized) == 0 {
			result = append(result, comparison)
			continue
		}

		firstClose := closes[0]
		comparison.FirstClose = normalized[0].Close
		comparison.LastClose = normalized[len(normalized)-1].Close
		comparison.TotalReturn = stringPointer(formatRat(subtractOne(divide(closes[len(closes)-1], firstClose))))
		comparison.Series = make([]ComparisonPoint, len(normalized))
		for index, candle := range normalized {
			comparison.Series[index] = ComparisonPoint{
				ObservedAt:      candle.ObservedAt,
				NormalizedClose: formatRat(scale(divide(closes[index], firstClose), 100)),
			}
		}
		result = append(result, comparison)
	}
	return result, nil
}

func normalizeCandles(candles []Candle) ([]Candle, []*big.Rat, error) {
	normalized := append([]Candle(nil), candles...)
	sort.SliceStable(normalized, func(left, right int) bool {
		return normalized[left].ObservedAt.Before(normalized[right].ObservedAt)
	})

	closes := make([]*big.Rat, len(normalized))
	for index, candle := range normalized {
		if candle.ObservedAt.IsZero() {
			return nil, nil, fmt.Errorf("candle %d observed_at is required", index)
		}
		if index > 0 && normalized[index-1].ObservedAt.Equal(candle.ObservedAt) {
			return nil, nil, fmt.Errorf("duplicate candle timestamp %s", candle.ObservedAt.UTC().Format(time.RFC3339))
		}
		closeValue, ok := new(big.Rat).SetString(strings.TrimSpace(candle.Close))
		if !ok || closeValue.Sign() <= 0 {
			return nil, nil, fmt.Errorf("candle %d close must be a positive decimal", index)
		}
		closes[index] = closeValue
		normalized[index].ObservedAt = candle.ObservedAt.UTC()
	}
	return normalized, closes, nil
}

func average(values []*big.Rat) *big.Rat {
	sum := new(big.Rat)
	for _, value := range values {
		sum.Add(sum, value)
	}
	return new(big.Rat).Quo(sum, big.NewRat(int64(len(values)), 1))
}

func annualizedVolatility(returns []*big.Rat) *big.Float {
	mean := average(returns)
	variance := new(big.Rat)
	for _, value := range returns {
		deviation := new(big.Rat).Sub(value, mean)
		variance.Add(variance, new(big.Rat).Mul(deviation, deviation))
	}
	variance.Quo(variance, big.NewRat(int64(len(returns)-1), 1))
	variance.Mul(variance, big.NewRat(annualTradingPeriods, 1))
	precise := new(big.Float).SetPrec(256).SetRat(variance)
	return new(big.Float).SetPrec(256).Sqrt(precise)
}

func latestVolatility(series []SeriesPoint) *string {
	for index := len(series) - 1; index >= 0; index-- {
		if series[index].Volatility20 != nil {
			value := *series[index].Volatility20
			return &value
		}
	}
	return nil
}

func applyWindow(result Result, candles []Candle, closes []*big.Rat, window analyticsWindow) Result {
	firstIndex := -1
	lastIndex := -1
	for index, candle := range candles {
		if !candle.ObservedAt.Before(window.from) && candle.ObservedAt.Before(window.to) {
			if firstIndex == -1 {
				firstIndex = index
			}
			lastIndex = index
		}
	}
	if firstIndex == -1 {
		return Result{Series: []SeriesPoint{}}
	}

	visibleSeries := append([]SeriesPoint(nil), result.Series[firstIndex:lastIndex+1]...)
	visibleCloses := closes[firstIndex : lastIndex+1]
	runningHigh := new(big.Rat).Set(visibleCloses[0])
	maximumDrawdown := new(big.Rat)
	for index, closeValue := range visibleCloses {
		visibleSeries[index].CumulativeReturn = stringPointer(formatRat(subtractOne(divide(closeValue, visibleCloses[0]))))
		if index == 0 {
			visibleSeries[index].PeriodReturn = nil
		}
		if closeValue.Cmp(runningHigh) > 0 {
			runningHigh.Set(closeValue)
		}
		drawdown := subtractOne(divide(closeValue, runningHigh))
		visibleSeries[index].Drawdown = stringPointer(formatRat(drawdown))
		if drawdown.Cmp(maximumDrawdown) < 0 {
			maximumDrawdown.Set(drawdown)
		}
	}

	result.Summary = Summary{
		CandleCount:          len(visibleCloses),
		FirstObservedAt:      candles[firstIndex].ObservedAt,
		LastObservedAt:       candles[lastIndex].ObservedAt,
		FirstClose:           candles[firstIndex].Close,
		LastClose:            candles[lastIndex].Close,
		TotalReturn:          stringPointer(formatRat(subtractOne(divide(visibleCloses[len(visibleCloses)-1], visibleCloses[0])))),
		MaximumDrawdown:      stringPointer(formatRat(maximumDrawdown)),
		AnnualizedVolatility: latestVolatility(visibleSeries),
	}
	result.Series = visibleSeries
	return result
}

func divide(left, right *big.Rat) *big.Rat {
	return new(big.Rat).Quo(left, right)
}

func subtractOne(value *big.Rat) *big.Rat {
	return new(big.Rat).Sub(value, big.NewRat(1, 1))
}

func scale(value *big.Rat, multiplier int64) *big.Rat {
	return new(big.Rat).Mul(value, big.NewRat(multiplier, 1))
}

func formatRat(value *big.Rat) string {
	return value.FloatString(outputDecimalPlaces)
}

func formatFloat(value *big.Float) string {
	return value.Text('f', outputDecimalPlaces)
}

func stringPointer(value string) *string {
	return &value
}
