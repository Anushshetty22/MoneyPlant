// Package ingestion contains provider adapters and provider-independent batch workflows.
package ingestion

import (
	// context carries cancellation and deadlines into the HTTP request.
	"context"
	// encoding/json decodes Yahoo Finance's nested chart response.
	"encoding/json"
	// fmt creates errors that identify malformed chart data or failed requests.
	"fmt"
	// io reads the response body and limits error-body output.
	"io"
	// net/http performs the Yahoo Finance chart request.
	"net/http"
	// net/url safely encodes the Yahoo symbol and query parameters.
	"net/url"
	// strconv converts Unix timestamps and numeric query values to text.
	"strconv"
	// strings validates symbols and parses raw decimal JSON values.
	"strings"
	// time creates UTC timestamps and a bounded default HTTP timeout.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// YahooFinanceChartBaseURL is the chart endpoint selected for the NSE EOD
	// fallback in the Phase 1 data-source decision.
	YahooFinanceChartBaseURL = "https://query1.finance.yahoo.com"

	// yahooRequestTimeout prevents a provider request from waiting indefinitely.
	yahooRequestTimeout = 10 * time.Second
)

// YahooMarketDataProvider fetches unadjusted historical EOD candles from Yahoo Finance.
//
// Phase 4.4 update: this adapter was added as a separate fallback provider for
// NSE symbols such as SBIN.NS. It implements the same HistoricalMarketDataProvider
// interface as Binance, allowing the pipeline to preserve the source identity
// without changing its normalized candle model.
type YahooMarketDataProvider struct {
	client  *http.Client
	baseURL string
}

// NewYahooMarketDataProvider creates a Yahoo Finance chart adapter.
//
// The HTTP client and endpoint are injectable for offline tests. A nil client
// receives a bounded default timeout, while a custom base URL lets tests use a
// local httptest server instead of contacting Yahoo Finance.
func NewYahooMarketDataProvider(client *http.Client, baseURL string) (*YahooMarketDataProvider, error) {
	if client == nil {
		client = &http.Client{Timeout: yahooRequestTimeout}
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid Yahoo Finance base URL %q", baseURL)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("Yahoo Finance base URL must use http or https, got %q", parsedURL.Scheme)
	}

	return &YahooMarketDataProvider{client: client, baseURL: baseURL}, nil
}

// ProviderName identifies Yahoo Finance in ingestion provenance and audit rows.
func (p *YahooMarketDataProvider) ProviderName() string {
	return "yahoo"
}

// FetchHistoricalCandles retrieves unadjusted Yahoo chart rows in [From, To).
//
// Yahoo's chart endpoint accepts Unix seconds through period1 and period2 and
// returns parallel timestamp and quote arrays. We filter the returned timestamps
// locally as an additional boundary guarantee because the provider's period2
// behavior may include an endpoint boundary depending on the requested interval.
func (p *YahooMarketDataProvider) FetchHistoricalCandles(
	ctx context.Context,
	request HistoricalCandleRequest,
) ([]database.MarketCandleInput, error) {
	if err := validateYahooRequest(request); err != nil {
		return nil, err
	}

	chart, err := p.fetchChart(ctx, request)
	if err != nil {
		return nil, err
	}
	if len(chart.Chart.Result) == 0 {
		return []database.MarketCandleInput{}, nil
	}

	result := chart.Chart.Result[0]
	if len(result.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("Yahoo chart response has no quote series")
	}
	quote := result.Indicators.Quote[0]
	if err := validateYahooSeriesLengths(result.Timestamp, quote); err != nil {
		return nil, err
	}

	retrievedAt := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	candles := make([]database.MarketCandleInput, 0, len(result.Timestamp))
	for index, unixSeconds := range result.Timestamp {
		observedAt := time.Unix(unixSeconds, 0).UTC()
		if observedAt.Before(request.From.Time.UTC()) || !observedAt.Before(request.To.Time.UTC()) {
			continue
		}

		candle, err := normalizeYahooQuote(quote, index, request.Interval, observedAt, retrievedAt)
		if err != nil {
			return nil, fmt.Errorf("normalize Yahoo row %d at %s: %w", index, observedAt.Format(time.RFC3339), err)
		}
		candles = append(candles, candle)
	}

	return candles, nil
}

// yahooChartResponse models only the response branches used by Phase 1.
// RawMessage arrays preserve the source decimal text, preventing an unnecessary
// float64 conversion before values reach PostgreSQL numeric columns.
type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []yahooQuote `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	} `json:"chart"`
}

// yahooQuote contains the unadjusted quote arrays selected for Phase 1.
// Adjusted close is intentionally not mapped because this fallback stores the
// raw OHLC values returned by Yahoo's quote series.
type yahooQuote struct {
	Open   []json.RawMessage `json:"open"`
	High   []json.RawMessage `json:"high"`
	Low    []json.RawMessage `json:"low"`
	Close  []json.RawMessage `json:"close"`
	Volume []json.RawMessage `json:"volume"`
}

// fetchChart builds one Yahoo chart request and decodes its JSON response.
func (p *YahooMarketDataProvider) fetchChart(ctx context.Context, request HistoricalCandleRequest) (yahooChartResponse, error) {
	endpoint, err := url.Parse(p.baseURL + "/v8/finance/chart/" + url.PathEscape(request.ProviderSymbol))
	if err != nil {
		return yahooChartResponse{}, fmt.Errorf("build Yahoo chart URL: %w", err)
	}

	query := endpoint.Query()
	query.Set("period1", strconv.FormatInt(request.From.Time.UTC().Unix(), 10))
	query.Set("period2", strconv.FormatInt(request.To.Time.UTC().Unix(), 10))
	query.Set("interval", "1d")
	query.Set("includeAdjustedClose", "true")
	query.Set("events", "history")
	endpoint.RawQuery = query.Encode()

	requestMessage, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return yahooChartResponse{}, fmt.Errorf("create Yahoo chart request: %w", err)
	}
	requestMessage.Header.Set("Accept", "application/json")

	response, err := p.client.Do(requestMessage)
	if err != nil {
		return yahooChartResponse{}, fmt.Errorf("request Yahoo chart for %s: %w", request.ProviderSymbol, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return yahooChartResponse{}, fmt.Errorf("Yahoo chart returned HTTP %d and error body could not be read: %w", response.StatusCode, readErr)
		}
		return yahooChartResponse{}, fmt.Errorf("Yahoo chart returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var chart yahooChartResponse
	if err := json.NewDecoder(response.Body).Decode(&chart); err != nil {
		return yahooChartResponse{}, fmt.Errorf("decode Yahoo chart response: %w", err)
	}
	if len(chart.Chart.Error) > 0 && string(chart.Chart.Error) != "null" {
		return yahooChartResponse{}, fmt.Errorf("Yahoo chart returned provider error: %s", string(chart.Chart.Error))
	}
	return chart, nil
}

// normalizeYahooQuote converts one parallel Yahoo quote position into the
// normalized market-candle model. SourceCloseAt and Binance-only fields remain
// invalid because Yahoo's EOD chart response does not provide those values.
func normalizeYahooQuote(
	quote yahooQuote,
	index int,
	interval string,
	observedAt time.Time,
	retrievedAt pgtype.Timestamptz,
) (database.MarketCandleInput, error) {
	openValue, err := yahooNumeric(quote.Open[index], "open")
	if err != nil {
		return database.MarketCandleInput{}, err
	}
	highValue, err := yahooNumeric(quote.High[index], "high")
	if err != nil {
		return database.MarketCandleInput{}, err
	}
	lowValue, err := yahooNumeric(quote.Low[index], "low")
	if err != nil {
		return database.MarketCandleInput{}, err
	}
	closeValue, err := yahooNumeric(quote.Close[index], "close")
	if err != nil {
		return database.MarketCandleInput{}, err
	}
	volumeValue, err := yahooNumeric(quote.Volume[index], "volume")
	if err != nil {
		return database.MarketCandleInput{}, err
	}

	return database.MarketCandleInput{
		Interval:          interval,
		ObservedAt:        pgtype.Timestamptz{Time: observedAt.UTC(), Valid: true},
		Open:              openValue,
		High:              highValue,
		Low:               lowValue,
		Close:             closeValue,
		Volume:            volumeValue,
		SourceRetrievedAt: retrievedAt,
	}, nil
}

// validateYahooRequest limits this Phase 1 fallback to daily NSE data and
// requires a bounded time range so period1/period2 are always deterministic.
func validateYahooRequest(request HistoricalCandleRequest) error {
	if strings.TrimSpace(request.ProviderSymbol) == "" {
		return fmt.Errorf("Yahoo provider symbol cannot be empty")
	}
	if request.Interval != "1d" {
		return fmt.Errorf("Yahoo fallback currently supports only the 1d interval, got %q", request.Interval)
	}
	if !request.From.Valid || !request.To.Valid {
		return fmt.Errorf("Yahoo historical requests require valid From and To timestamps")
	}
	if !request.From.Time.Before(request.To.Time) {
		return fmt.Errorf("Yahoo request To must be after From")
	}
	return nil
}

// validateYahooSeriesLengths prevents a short or misaligned quote array from
// silently assigning one trading day's value to another day's timestamp.
func validateYahooSeriesLengths(timestamps []int64, quote yahooQuote) error {
	if len(timestamps) == 0 {
		return nil
	}
	for name, values := range map[string][]json.RawMessage{
		"open":   quote.Open,
		"high":   quote.High,
		"low":    quote.Low,
		"close":  quote.Close,
		"volume": quote.Volume,
	} {
		if len(values) != len(timestamps) {
			return fmt.Errorf("Yahoo %s series length = %d, want %d timestamps", name, len(values), len(timestamps))
		}
	}
	return nil
}

// yahooNumeric parses either a JSON number or a quoted decimal into pgtype.Numeric.
// A JSON null is rejected because OHLCV rows with missing required values must
// not be converted into zeroes and stored as if they were real market data.
func yahooNumeric(raw json.RawMessage, fieldName string) (pgtype.Numeric, error) {
	valueText := strings.TrimSpace(string(raw))
	if valueText == "" || valueText == "null" {
		return pgtype.Numeric{}, fmt.Errorf("Yahoo %s value is null or empty", fieldName)
	}
	if strings.HasPrefix(valueText, "\"") {
		var quotedValue string
		if err := json.Unmarshal(raw, &quotedValue); err != nil {
			return pgtype.Numeric{}, fmt.Errorf("decode Yahoo %s decimal: %w", fieldName, err)
		}
		valueText = quotedValue
	}

	var numericValue pgtype.Numeric
	if err := numericValue.Scan(valueText); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("parse Yahoo %s value %q: %w", fieldName, valueText, err)
	}
	return numericValue, nil
}
