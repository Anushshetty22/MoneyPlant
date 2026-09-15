package analytics

import (
	"strings"
	"testing"
	"time"
)

func TestCalculateMarketAnalytics(t *testing.T) {
	result, err := Calculate([]Candle{
		{ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Close: "100.00"},
		{ObservedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Close: "110.00"},
		{ObservedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Close: "105.00"},
		{ObservedAt: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), Close: "120.00"},
	})
	if err != nil {
		t.Fatalf("calculate analytics: %v", err)
	}
	if result.Summary.CandleCount != 4 {
		t.Fatalf("candle count = %d, want 4", result.Summary.CandleCount)
	}
	assertMetric(t, result.Series[0].CumulativeReturn, "0.0000000000")
	assertMetric(t, result.Series[1].PeriodReturn, "0.1000000000")
	assertMetric(t, result.Series[2].PeriodReturn, "-0.0454545455")
	assertMetric(t, result.Summary.TotalReturn, "0.2000000000")
	assertMetric(t, result.Summary.MaximumDrawdown, "-0.0454545455")
	assertMetric(t, result.Series[2].Drawdown, "-0.0454545455")
	if result.Series[0].PeriodReturn != nil {
		t.Fatal("first period return should be nil")
	}
	if result.Series[3].Volatility20 != nil {
		t.Fatal("volatility should be nil before 20 returns")
	}
}

func TestCalculateWarmupUsesObservationCount(t *testing.T) {
	candles := make([]Candle, 0, 51)
	for index := 0; index < 51; index++ {
		candle := Candle{
			ObservedAt: time.Date(2026, 1, 1+index*2, 0, 0, 0, 0, time.UTC),
			Close:      "100.00",
		}
		candles = append(candles, candle)
	}

	result, err := Calculate(candles)
	if err != nil {
		t.Fatalf("calculate warmup analytics: %v", err)
	}
	if result.Series[5].SMA7 != nil || result.Series[6].SMA7 == nil {
		t.Fatal("SMA7 warmup did not use seven observations")
	}
	if result.Series[48].SMA50 != nil || result.Series[49].SMA50 == nil {
		t.Fatal("SMA50 warmup did not use fifty observations")
	}
	if result.Series[50].Volatility20 == nil {
		t.Fatal("constant series should have a defined 20-return volatility after warmup")
	}
	assertMetric(t, result.Series[50].Volatility20, "0.0000000000")
}

func TestCalculateWindowUsesLookbackForIndicators(t *testing.T) {
	candles := make([]Candle, 0, 51)
	for index := 0; index < 51; index++ {
		candles = append(candles, Candle{
			ObservedAt: time.Date(2026, 5, 1+index, 0, 0, 0, 0, time.UTC),
			Close:      "100.00",
		})
	}
	from := time.Date(2026, 5, 51, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	result, err := CalculateWindow(candles, from, to)
	if err != nil {
		t.Fatalf("calculate analytics window: %v", err)
	}
	if len(result.Series) != 1 || result.Summary.CandleCount != 1 {
		t.Fatalf("window result = %#v, want one visible candle", result)
	}
	if result.Series[0].SMA50 == nil || result.Series[0].Volatility20 == nil {
		t.Fatal("lookback candles did not warm up rolling metrics")
	}
	if result.Series[0].PeriodReturn != nil || result.Series[0].CumulativeReturn == nil || *result.Series[0].CumulativeReturn != "0.0000000000" {
		t.Fatalf("window boundary returns = period=%v cumulative=%v, want nil/zero", result.Series[0].PeriodReturn, result.Series[0].CumulativeReturn)
	}
	if result.Series[0].ObservedAt != from {
		t.Fatalf("visible timestamp = %s, want %s", result.Series[0].ObservedAt, from)
	}
}

func TestCalculateVolatilityAndDrawdown(t *testing.T) {
	candles := make([]Candle, 0, 22)
	for index := 0; index < 22; index++ {
		closeValue := "100.00"
		if index%2 == 1 {
			closeValue = "110.00"
		}
		candles = append(candles, Candle{
			ObservedAt: time.Date(2026, 2, 1+index, 0, 0, 0, 0, time.UTC),
			Close:      closeValue,
		})
	}

	result, err := Calculate(candles)
	if err != nil {
		t.Fatalf("calculate volatility analytics: %v", err)
	}
	if result.Series[19].Volatility20 != nil {
		t.Fatal("volatility should require twenty returns")
	}
	if result.Series[20].Volatility20 == nil {
		t.Fatal("volatility should be present after twenty returns")
	}
	if result.Summary.MaximumDrawdown == nil || !strings.HasPrefix(*result.Summary.MaximumDrawdown, "-0.0909090909") {
		t.Fatalf("maximum drawdown = %v, want approximately -0.0909090909", result.Summary.MaximumDrawdown)
	}
}

func TestCalculateRejectsInvalidAndDuplicateCandles(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		candles []Candle
	}{
		{
			name:    "zero close",
			candles: []Candle{{ObservedAt: base, Close: "0"}},
		},
		{
			name: "duplicate timestamp",
			candles: []Candle{
				{ObservedAt: base, Close: "100"},
				{ObservedAt: base, Close: "101"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Calculate(test.candles); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestCompareNormalizesEachSeries(t *testing.T) {
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	result, err := Compare([]ComparisonInput{
		{
			Symbol: "btcusdt",
			Candles: []Candle{
				{ObservedAt: base, Close: "100"},
				{ObservedAt: base.Add(24 * time.Hour), Close: "110"},
			},
		},
		{
			Symbol: "ETHUSDT",
			Candles: []Candle{
				{ObservedAt: base, Close: "200"},
				{ObservedAt: base.Add(24 * time.Hour), Close: "180"},
			},
		},
	})
	if err != nil {
		t.Fatalf("compare analytics: %v", err)
	}
	if len(result) != 2 || result[0].Symbol != "BTCUSDT" {
		t.Fatalf("comparison result = %#v", result)
	}
	if got := result[0].Series[0].NormalizedClose; got != "100.0000000000" {
		t.Fatalf("BTCUSDT starting index = %s, want 100.0000000000", got)
	}
	if got := result[0].Series[1].NormalizedClose; got != "110.0000000000" {
		t.Fatalf("BTCUSDT ending index = %s, want 110.0000000000", got)
	}
	assertMetric(t, result[1].TotalReturn, "-0.1000000000")
}

func assertMetric(t *testing.T, value *string, want string) {
	t.Helper()
	if value == nil {
		t.Fatalf("metric is nil, want %s", want)
	}
	if *value != want {
		t.Fatalf("metric = %s, want %s", *value, want)
	}
}
