// Package ingestion contains provider adapters and provider-independent batch workflows.
package ingestion

import (
	// context carries cancellation and deadlines into every HTTP request.
	"context"
	// encoding/json decodes Binance's array-based kline response.
	"encoding/json"
	// fmt creates errors that identify the failed Binance field or operation.
	"fmt"
	// io reads the HTTP response body and limits error-body output.
	"io"
	// net/http performs public HTTPS requests without requiring an API key.
	"net/http"
	// net/url safely encodes symbols, intervals, and timestamp query parameters.
	"net/url"
	// strconv parses integer values that may be returned as JSON numbers or strings.
	"strconv"
	// strings normalizes and validates the configurable endpoint URL.
	"strings"
	// time converts Binance millisecond timestamps into UTC time values.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// BinancePublicDataBaseURL is the documented market-data-only endpoint.
	// It is a variable constructor default rather than a hard-coded request target,
	// so tests can replace it with an httptest server and future environments can
	// select another Binance-compatible endpoint.
	BinancePublicDataBaseURL = "https://data-api.binance.vision"

	// binanceKlineLimit is Binance's maximum number of rows returned by one
	// /api/v3/klines request according to the public Spot REST API contract.
	binanceKlineLimit = 1000

	// binanceRequestTimeout prevents a provider call from waiting forever when
	// the remote service or network is unavailable.
	binanceRequestTimeout = 10 * time.Second
)

// BinanceMarketDataProvider fetches public historical Spot klines from Binance.
//
// Phase 4.2 update: this real provider was added beside FixtureMarketDataProvider.
// Both satisfy HistoricalMarketDataProvider, so the existing ingestion service
// does not need to know whether rows came from an HTTP API or an offline fixture.
type BinanceMarketDataProvider struct {
	client  *http.Client
	baseURL string
}

// NewBinanceMarketDataProvider creates a Binance adapter.
//
// The caller may provide a custom HTTP client for tests or shared transport
// settings. A nil client receives a safe default timeout. The base URL is also
// injectable because tests must not call the real internet and deployments may
// choose a different documented Binance market-data endpoint.
func NewBinanceMarketDataProvider(client *http.Client, baseURL string) (*BinanceMarketDataProvider, error) {
	if client == nil {
		client = &http.Client{Timeout: binanceRequestTimeout}
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid Binance base URL %q", baseURL)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("Binance base URL must use http or https, got %q", parsedURL.Scheme)
	}

	return &BinanceMarketDataProvider{client: client, baseURL: baseURL}, nil
}

// ProviderName identifies Binance in ingestion_runs and source provenance.
func (p *BinanceMarketDataProvider) ProviderName() string {
	return "binance"
}

// FetchHistoricalCandles retrieves a half-open [From, To) time range.
//
// Binance's endTime is inclusive, while MoneyPlant uses an exclusive To bound.
// Therefore this adapter sends To minus one millisecond. If the range contains
// more than 1,000 candles, the next request starts one millisecond after the
// last returned open time. That gives the API a forward-only cursor and avoids
// downloading the same kline twice.
func (p *BinanceMarketDataProvider) FetchHistoricalCandles(
	ctx context.Context,
	request HistoricalCandleRequest,
) ([]database.MarketCandleInput, error) {
	if err := validateBinanceRequest(request); err != nil {
		return nil, err
	}

	startMillis := request.From.Time.UTC().UnixMilli()
	endMillis := request.To.Time.UTC().UnixMilli() - 1
	retrievedAt := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	candles := make([]database.MarketCandleInput, 0)

	for {
		// The context check makes cancellation responsive between pages as well
		// as during the individual HTTP request.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		rows, err := p.fetchKlinePage(ctx, request.ProviderSymbol, request.Interval, startMillis, endMillis)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}

		for rowIndex, row := range rows {
			candle, err := normalizeBinanceKline(row, request.Interval, retrievedAt)
			if err != nil {
				return nil, fmt.Errorf("normalize Binance kline row %d: %w", rowIndex, err)
			}
			candles = append(candles, candle)
		}

		// A short page proves that Binance has no more rows in the requested
		// range. This avoids an unnecessary follow-up request.
		if len(rows) < binanceKlineLimit {
			break
		}

		lastOpenMillis, err := binanceKlineTimestamp(rows[len(rows)-1], 0)
		if err != nil {
			return nil, fmt.Errorf("read last Binance kline open time: %w", err)
		}
		nextStartMillis := lastOpenMillis + 1
		if nextStartMillis <= startMillis {
			return nil, fmt.Errorf("Binance pagination did not advance beyond %d", startMillis)
		}
		if nextStartMillis > endMillis {
			break
		}
		startMillis = nextStartMillis
	}

	return candles, nil
}

// fetchKlinePage builds and executes one Binance REST request.
//
// The response is intentionally kept as json.RawMessage values. Binance
// returns prices and volumes as strings for decimal precision, while timestamps
// and trade counts are JSON numbers. RawMessage lets the normalizer parse each
// field using the correct type instead of converting financial values through
// float64 and risking precision loss.
func (p *BinanceMarketDataProvider) fetchKlinePage(
	ctx context.Context,
	symbol string,
	interval string,
	startMillis int64,
	endMillis int64,
) ([][]json.RawMessage, error) {
	endpoint, err := url.Parse(p.baseURL + "/api/v3/klines")
	if err != nil {
		return nil, fmt.Errorf("build Binance klines URL: %w", err)
	}

	query := endpoint.Query()
	query.Set("symbol", symbol)
	query.Set("interval", interval)
	query.Set("startTime", strconv.FormatInt(startMillis, 10))
	query.Set("endTime", strconv.FormatInt(endMillis, 10))
	query.Set("limit", strconv.Itoa(binanceKlineLimit))
	// Binance interprets timeZone independently from startTime/endTime. An
	// explicit UTC value makes our timestamp policy visible in the request.
	query.Set("timeZone", "0")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Binance klines request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	response, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Binance klines for %s: %w", symbol, err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return nil, fmt.Errorf("Binance klines returned HTTP %d and error body could not be read: %w", response.StatusCode, readErr)
		}
		return nil, fmt.Errorf("Binance klines returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var rows [][]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode Binance klines response: %w", err)
	}
	return rows, nil
}

// normalizeBinanceKline maps Binance's documented 12-position tuple into the
// provider-neutral database input model.
//
// Positions 0, 6, and 8 are open time, close time, and trade count. Positions
// 1 through 5 are OHLCV values. Positions 7, 9, and 10 preserve Binance-only
// quote-volume and taker-buy metrics. Position 11 is deliberately ignored
// because Binance documents it as an unused field.
func normalizeBinanceKline(
	row []json.RawMessage,
	interval string,
	retrievedAt pgtype.Timestamptz,
) (database.MarketCandleInput, error) {
	if len(row) != 12 {
		return database.MarketCandleInput{}, fmt.Errorf("expected 12 fields, got %d", len(row))
	}

	openTime, err := binanceKlineTimestamp(row, 0)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("open time: %w", err)
	}
	closeTime, err := binanceKlineTimestamp(row, 6)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("close time: %w", err)
	}

	openValue, err := binanceNumeric(row, 1)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("open: %w", err)
	}
	highValue, err := binanceNumeric(row, 2)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("high: %w", err)
	}
	lowValue, err := binanceNumeric(row, 3)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("low: %w", err)
	}
	closeValue, err := binanceNumeric(row, 4)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("close: %w", err)
	}
	volumeValue, err := binanceNumeric(row, 5)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("volume: %w", err)
	}
	quoteVolumeValue, err := binanceNumeric(row, 7)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("quote volume: %w", err)
	}
	tradeCount, err := binanceInt8(row, 8)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("trade count: %w", err)
	}
	takerBuyVolumeValue, err := binanceNumeric(row, 9)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("taker buy volume: %w", err)
	}
	takerBuyQuoteVolumeValue, err := binanceNumeric(row, 10)
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("taker buy quote volume: %w", err)
	}

	return database.MarketCandleInput{
		Interval:            interval,
		ObservedAt:          timestampFromMillis(openTime),
		SourceCloseAt:       timestampFromMillis(closeTime),
		Open:                openValue,
		High:                highValue,
		Low:                 lowValue,
		Close:               closeValue,
		Volume:              volumeValue,
		QuoteVolume:         quoteVolumeValue,
		TradeCount:          tradeCount,
		TakerBuyVolume:      takerBuyVolumeValue,
		TakerBuyQuoteVolume: takerBuyQuoteVolumeValue,
		SourceRetrievedAt:   retrievedAt,
	}, nil
}

// validateBinanceRequest checks assumptions that keep pagination and timestamp
// conversion deterministic before any network request is made.
func validateBinanceRequest(request HistoricalCandleRequest) error {
	if strings.TrimSpace(request.ProviderSymbol) == "" {
		return fmt.Errorf("Binance provider symbol cannot be empty")
	}
	if !binanceIntervals[request.Interval] {
		return fmt.Errorf("unsupported Binance interval %q", request.Interval)
	}
	if !request.From.Valid || !request.To.Valid {
		return fmt.Errorf("Binance historical requests require valid From and To timestamps")
	}
	if !request.From.Time.Before(request.To.Time) {
		return fmt.Errorf("Binance request To must be after From")
	}
	return nil
}

// binanceIntervals mirrors the Phase 1 market_candles interval constraint.
// Intervals supported by Binance but not selected for our database are rejected
// here instead of failing later at PostgreSQL constraint validation.
var binanceIntervals = map[string]bool{
	"1m":  true,
	"5m":  true,
	"15m": true,
	"30m": true,
	"1h":  true,
	"4h":  true,
	"1d":  true,
	"1w":  true,
}

// binanceKlineTimestamp parses a millisecond timestamp at one tuple position.
func binanceKlineTimestamp(row []json.RawMessage, index int) (int64, error) {
	if index >= len(row) {
		return 0, fmt.Errorf("field index %d is missing", index)
	}

	var numericValue int64
	if err := json.Unmarshal(row[index], &numericValue); err == nil {
		return numericValue, nil
	}

	var stringValue string
	if err := json.Unmarshal(row[index], &stringValue); err != nil {
		return 0, fmt.Errorf("field %d is not an integer timestamp: %w", index, err)
	}
	numericValue, err := strconv.ParseInt(stringValue, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("field %d is not an integer timestamp: %w", index, err)
	}
	return numericValue, nil
}

// binanceNumeric parses a decimal string directly into pgtype.Numeric so the
// exact source precision is retained for PostgreSQL numeric columns.
func binanceNumeric(row []json.RawMessage, index int) (pgtype.Numeric, error) {
	if index >= len(row) {
		return pgtype.Numeric{}, fmt.Errorf("field index %d is missing", index)
	}
	var value string
	if err := json.Unmarshal(row[index], &value); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("field %d is not a decimal string: %w", index, err)
	}

	var numericValue pgtype.Numeric
	if err := numericValue.Scan(value); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("parse decimal %q: %w", value, err)
	}
	return numericValue, nil
}

// binanceInt8 parses trade count into pgtype.Int8, preserving the nullable
// database representation even though Binance supplies this field for klines.
func binanceInt8(row []json.RawMessage, index int) (pgtype.Int8, error) {
	if index >= len(row) {
		return pgtype.Int8{}, fmt.Errorf("field index %d is missing", index)
	}

	var numericValue int64
	if err := json.Unmarshal(row[index], &numericValue); err != nil {
		var stringValue string
		if stringErr := json.Unmarshal(row[index], &stringValue); stringErr != nil {
			return pgtype.Int8{}, fmt.Errorf("field %d is not an integer: %w", index, err)
		}
		numericValue, err = strconv.ParseInt(stringValue, 10, 64)
		if err != nil {
			return pgtype.Int8{}, fmt.Errorf("field %d is not an integer: %w", index, err)
		}
	}

	return pgtype.Int8{Int64: numericValue, Valid: true}, nil
}

// timestampFromMillis converts Binance's UTC millisecond timestamp to pgtype.
func timestampFromMillis(milliseconds int64) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  time.UnixMilli(milliseconds).UTC(),
		Valid: true,
	}
}
