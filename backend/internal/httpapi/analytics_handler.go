package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/analytics"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

const analyticsLookbackLimit int32 = 50

type analyticsCandleRepository interface {
	ListByCanonicalSymbol(context.Context, string, string, string, pgtype.Timestamptz, pgtype.Timestamptz) ([]database.MarketCandle, error)
	ListBeforeCanonicalSymbol(context.Context, string, string, string, pgtype.Timestamptz, int32) ([]database.MarketCandle, error)
}

func analyticsRepositoryFromConcrete(repository *database.MarketCandleRepository) analyticsCandleRepository {
	if repository == nil {
		return nil
	}
	return repository
}

type analyticsRequest struct {
	Provider string
	Interval string
	From     time.Time
	To       time.Time
}

type marketAnalyticsResponse struct {
	CanonicalSymbol string                          `json:"canonical_symbol"`
	Provider        string                          `json:"provider"`
	Interval        string                          `json:"interval"`
	From            string                          `json:"from"`
	To              string                          `json:"to"`
	Summary         *marketAnalyticsSummary         `json:"summary"`
	Series          []marketAnalyticsSeriesResponse `json:"series"`
}

type marketAnalyticsSummary struct {
	CandleCount          int     `json:"candle_count"`
	FirstObservedAt      string  `json:"first_observed_at"`
	LastObservedAt       string  `json:"last_observed_at"`
	FirstClose           string  `json:"first_close"`
	LastClose            string  `json:"last_close"`
	TotalReturn          *string `json:"total_return"`
	MaximumDrawdown      *string `json:"maximum_drawdown"`
	AnnualizedVolatility *string `json:"annualized_volatility_20"`
}

type marketAnalyticsSeriesResponse struct {
	ObservedAt       string  `json:"observed_at"`
	Close            string  `json:"close"`
	PeriodReturn     *string `json:"period_return"`
	CumulativeReturn *string `json:"cumulative_return"`
	SMA7             *string `json:"sma_7"`
	SMA20            *string `json:"sma_20"`
	SMA50            *string `json:"sma_50"`
	Volatility20     *string `json:"volatility_20"`
	Drawdown         *string `json:"drawdown"`
}

type comparisonResponse struct {
	Provider string                     `json:"provider"`
	Interval string                     `json:"interval"`
	From     string                     `json:"from"`
	To       string                     `json:"to"`
	Series   []comparisonSeriesResponse `json:"series"`
}

type comparisonSeriesResponse struct {
	CanonicalSymbol string                    `json:"canonical_symbol"`
	FirstClose      string                    `json:"first_close"`
	LastClose       string                    `json:"last_close"`
	TotalReturn     *string                   `json:"total_return"`
	Series          []comparisonPointResponse `json:"series"`
}

type comparisonPointResponse struct {
	ObservedAt      string `json:"observed_at"`
	NormalizedClose string `json:"normalized_close"`
}

func listMarketAnalyticsHandler(
	responseWriter http.ResponseWriter,
	request *http.Request,
	repository analyticsCandleRepository,
) {
	if repository == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "market analytics repository is not configured",
		})
		return
	}

	query := request.URL.Query()
	symbol := strings.ToUpper(strings.TrimSpace(query.Get("symbol")))
	if symbol == "" {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": "symbol is required"})
		return
	}
	analyticsQuery, err := parseAnalyticsRequest(query)
	if err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	candles, err := loadAnalyticsCandles(request.Context(), repository, symbol, analyticsQuery.Provider, analyticsQuery.Interval, analyticsQuery.From, analyticsQuery.To, true)
	if err != nil {
		log.Printf("load analytics candles for %s via %s: %v", symbol, analyticsQuery.Provider, err)
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{"error": "unable to load market analytics"})
		return
	}

	result, err := analytics.CalculateWindow(analyticsInputs(candles), analyticsQuery.From, analyticsQuery.To)
	if err != nil {
		log.Printf("calculate analytics for %s via %s: %v", symbol, analyticsQuery.Provider, err)
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{"error": "unable to calculate market analytics"})
		return
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{
		"data": marketAnalyticsResponseFromResult(symbol, analyticsQuery, result),
	})
}

func compareMarketAnalyticsHandler(
	responseWriter http.ResponseWriter,
	request *http.Request,
	repository analyticsCandleRepository,
) {
	if repository == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "market analytics repository is not configured",
		})
		return
	}

	query := request.URL.Query()
	symbols, err := parseComparisonSymbols(query.Get("symbols"))
	if err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	analyticsQuery, err := parseAnalyticsRequest(query)
	if err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	inputs := make([]analytics.ComparisonInput, 0, len(symbols))
	for _, symbol := range symbols {
		candles, loadErr := loadAnalyticsCandles(request.Context(), repository, symbol, analyticsQuery.Provider, analyticsQuery.Interval, analyticsQuery.From, analyticsQuery.To, false)
		if loadErr != nil {
			log.Printf("load comparison candles for %s via %s: %v", symbol, analyticsQuery.Provider, loadErr)
			writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{"error": "unable to load market comparison"})
			return
		}
		inputs = append(inputs, analytics.ComparisonInput{Symbol: symbol, Candles: analyticsInputs(candles)})
	}

	comparison, err := analytics.Compare(inputs)
	if err != nil {
		log.Printf("calculate market comparison: %v", err)
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{"error": "unable to calculate market comparison"})
		return
	}

	series := make([]comparisonSeriesResponse, 0, len(comparison))
	for _, item := range comparison {
		points := make([]comparisonPointResponse, 0, len(item.Series))
		for _, point := range item.Series {
			points = append(points, comparisonPointResponse{
				ObservedAt:      formatTimestampValue(point.ObservedAt),
				NormalizedClose: point.NormalizedClose,
			})
		}
		series = append(series, comparisonSeriesResponse{
			CanonicalSymbol: item.Symbol,
			FirstClose:      item.FirstClose,
			LastClose:       item.LastClose,
			TotalReturn:     item.TotalReturn,
			Series:          points,
		})
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{
		"data": comparisonResponse{
			Provider: analyticsQuery.Provider,
			Interval: analyticsQuery.Interval,
			From:     formatTimestampValue(analyticsQuery.From),
			To:       formatTimestampValue(analyticsQuery.To),
			Series:   series,
		},
	})
}

func parseAnalyticsRequest(query map[string][]string) (analyticsRequest, error) {
	provider := strings.ToLower(strings.TrimSpace(firstQueryValue(query, "provider")))
	if provider == "" {
		return analyticsRequest{}, fmt.Errorf("provider is required")
	}
	interval := strings.TrimSpace(firstQueryValue(query, "interval"))
	if interval == "" {
		interval = "1d"
	}
	if interval != "1d" {
		return analyticsRequest{}, fmt.Errorf("analytics supports only interval 1d")
	}
	fromText := strings.TrimSpace(firstQueryValue(query, "from"))
	toText := strings.TrimSpace(firstQueryValue(query, "to"))
	if fromText == "" || toText == "" {
		return analyticsRequest{}, fmt.Errorf("from and to are required")
	}
	from, err := parseRFC3339QueryTime("from", fromText)
	if err != nil {
		return analyticsRequest{}, err
	}
	to, err := parseRFC3339QueryTime("to", toText)
	if err != nil {
		return analyticsRequest{}, err
	}
	if !from.Before(to) {
		return analyticsRequest{}, fmt.Errorf("to must be after from")
	}
	return analyticsRequest{Provider: provider, Interval: interval, From: from, To: to}, nil
}

func parseComparisonSymbols(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("symbols is required")
	}
	if len(parts) > 8 {
		return nil, fmt.Errorf("symbols cannot contain more than 8 instruments")
	}
	seen := make(map[string]struct{}, len(parts))
	symbols := make([]string, 0, len(parts))
	for _, part := range parts {
		symbol := strings.ToUpper(strings.TrimSpace(part))
		if symbol == "" {
			return nil, fmt.Errorf("symbols cannot contain an empty instrument")
		}
		if _, exists := seen[symbol]; exists {
			return nil, fmt.Errorf("symbol %q is duplicated", symbol)
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	return symbols, nil
}

func loadAnalyticsCandles(
	ctx context.Context,
	repository analyticsCandleRepository,
	symbol string,
	provider string,
	interval string,
	from time.Time,
	to time.Time,
	includeLookback bool,
) ([]database.MarketCandle, error) {
	fromTimestamp := pgtype.Timestamptz{Time: from.UTC(), Valid: true}
	toTimestamp := pgtype.Timestamptz{Time: to.UTC(), Valid: true}
	requested, err := repository.ListByCanonicalSymbol(ctx, symbol, provider, interval, fromTimestamp, toTimestamp)
	if err != nil {
		return nil, err
	}
	if !includeLookback {
		return requested, nil
	}
	lookback, err := repository.ListBeforeCanonicalSymbol(ctx, symbol, provider, interval, fromTimestamp, analyticsLookbackLimit)
	if err != nil {
		return nil, err
	}
	combined := append(lookback, requested...)
	sort.SliceStable(combined, func(left, right int) bool {
		return combined[left].ObservedAt.Time.Before(combined[right].ObservedAt.Time)
	})
	return combined, nil
}

func analyticsInputs(candles []database.MarketCandle) []analytics.Candle {
	inputs := make([]analytics.Candle, 0, len(candles))
	for _, candle := range candles {
		inputs = append(inputs, analytics.Candle{
			ObservedAt: candle.ObservedAt.Time.UTC(),
			Close:      requiredNumeric(candle.Close),
		})
	}
	return inputs
}

func marketAnalyticsResponseFromResult(symbol string, query analyticsRequest, result analytics.Result) marketAnalyticsResponse {
	response := marketAnalyticsResponse{
		CanonicalSymbol: symbol,
		Provider:        query.Provider,
		Interval:        query.Interval,
		From:            formatTimestampValue(query.From),
		To:              formatTimestampValue(query.To),
		Series:          make([]marketAnalyticsSeriesResponse, 0, len(result.Series)),
	}
	if result.Summary.CandleCount > 0 {
		response.Summary = &marketAnalyticsSummary{
			CandleCount:          result.Summary.CandleCount,
			FirstObservedAt:      formatTimestampValue(result.Summary.FirstObservedAt),
			LastObservedAt:       formatTimestampValue(result.Summary.LastObservedAt),
			FirstClose:           result.Summary.FirstClose,
			LastClose:            result.Summary.LastClose,
			TotalReturn:          result.Summary.TotalReturn,
			MaximumDrawdown:      result.Summary.MaximumDrawdown,
			AnnualizedVolatility: result.Summary.AnnualizedVolatility,
		}
	}
	for _, point := range result.Series {
		response.Series = append(response.Series, marketAnalyticsSeriesResponse{
			ObservedAt:       formatTimestampValue(point.ObservedAt),
			Close:            point.Close,
			PeriodReturn:     point.PeriodReturn,
			CumulativeReturn: point.CumulativeReturn,
			SMA7:             point.SMA7,
			SMA20:            point.SMA20,
			SMA50:            point.SMA50,
			Volatility20:     point.Volatility20,
			Drawdown:         point.Drawdown,
		})
	}
	return response
}

func formatTimestampValue(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func firstQueryValue(query map[string][]string, key string) string {
	values := query[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
