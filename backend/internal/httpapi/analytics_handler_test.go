package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestListMarketAnalyticsHandlerUsesLookbackAndReturnsRequestedWindow(t *testing.T) {
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	repository := &fakeAnalyticsCandleRepository{}
	for index := 0; index < 51; index++ {
		repository.candles = append(repository.candles, analyticsTestCandle(t, base.Add(time.Duration(index)*24*time.Hour), 100+index))
	}
	from := base.Add(50 * 24 * time.Hour)
	to := from.Add(24 * time.Hour)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/market?symbol=BTCUSDT&provider=binance&from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339), nil)
	response := httptest.NewRecorder()

	listMarketAnalyticsHandler(response, request, repository)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Summary *struct {
				CandleCount int `json:"candle_count"`
			} `json:"summary"`
			Series []struct {
				ObservedAt string  `json:"observed_at"`
				SMA50      *string `json:"sma_50"`
			} `json:"series"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode analytics response: %v; body=%s", err, response.Body.String())
	}
	if envelope.Data.Summary == nil || envelope.Data.Summary.CandleCount != 1 {
		t.Fatalf("summary = %#v, want one visible candle", envelope.Data.Summary)
	}
	if len(envelope.Data.Series) != 1 || envelope.Data.Series[0].ObservedAt != from.Format(time.RFC3339) {
		t.Fatalf("series = %#v, want one requested candle", envelope.Data.Series)
	}
	if envelope.Data.Series[0].SMA50 == nil {
		t.Fatal("SMA50 should be available from the 50-candle lookback")
	}
	if repository.lookbackLimit != 50 {
		t.Fatalf("lookback limit = %d, want 50", repository.lookbackLimit)
	}
}

func TestMarketAnalyticsHandlersValidateRequests(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "missing symbol", path: "/api/v1/analytics/market?provider=binance&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z"},
		{name: "missing provider", path: "/api/v1/analytics/market?symbol=BTCUSDT&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z"},
		{name: "unsupported interval", path: "/api/v1/analytics/market?symbol=BTCUSDT&provider=binance&interval=1h&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z"},
		{name: "reversed range", path: "/api/v1/analytics/market?symbol=BTCUSDT&provider=binance&from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z"},
		{name: "duplicate comparison", path: "/api/v1/analytics/compare?symbols=BTCUSDT,btcusdt&provider=binance&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if strings.Contains(test.path, "/compare") {
				compareMarketAnalyticsHandler(response, request, &fakeAnalyticsCandleRepository{})
			} else {
				listMarketAnalyticsHandler(response, request, &fakeAnalyticsCandleRepository{})
			}
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestCompareMarketAnalyticsHandlerReturnsNormalizedSeries(t *testing.T) {
	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	repository := &fakeAnalyticsCandleRepository{
		candles: []database.MarketCandle{
			analyticsTestCandle(t, base, 100),
			analyticsTestCandle(t, base.Add(24*time.Hour), 110),
			analyticsTestCandle(t, base, 200),
			analyticsTestCandle(t, base.Add(24*time.Hour), 180),
		},
	}
	// The fake identifies the requested canonical symbol through the test-only
	// source ID, so expose separate slices with the same dates below.
	repository.symbolCandles = map[string][]database.MarketCandle{
		"BTCUSDT": {analyticsTestCandle(t, base, 100), analyticsTestCandle(t, base.Add(24*time.Hour), 110)},
		"ETHUSDT": {analyticsTestCandle(t, base, 200), analyticsTestCandle(t, base.Add(24*time.Hour), 180)},
	}
	from := base
	to := base.Add(2 * 24 * time.Hour)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/compare?symbols=BTCUSDT,ETHUSDT&provider=binance&from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339), nil)
	response := httptest.NewRecorder()

	compareMarketAnalyticsHandler(response, request, repository)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Series []struct {
				CanonicalSymbol string `json:"canonical_symbol"`
				Series          []struct {
					NormalizedClose string `json:"normalized_close"`
				} `json:"series"`
			} `json:"series"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode comparison response: %v; body=%s", err, response.Body.String())
	}
	if len(envelope.Data.Series) != 2 || envelope.Data.Series[0].CanonicalSymbol != "BTCUSDT" {
		t.Fatalf("comparison series = %#v, want BTCUSDT and ETHUSDT", envelope.Data.Series)
	}
	if got := envelope.Data.Series[0].Series[1].NormalizedClose; got != "110.0000000000" {
		t.Fatalf("BTCUSDT normalized close = %s, want 110.0000000000", got)
	}
}

func TestListMarketAnalyticsHandlerReturnsEmptyResult(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/market?symbol=BTCUSDT&provider=binance&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", nil)
	response := httptest.NewRecorder()

	listMarketAnalyticsHandler(response, request, &fakeAnalyticsCandleRepository{})

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"summary":null`) || !strings.Contains(response.Body.String(), `"series":[]`) {
		t.Fatalf("empty analytics response = %s", response.Body.String())
	}
}

func TestAnalyticsRoutesReturnConfigurationErrorWithoutRepository(t *testing.T) {
	server := NewServer("127.0.0.1", 0, nil, nil, nil, nil, nil, nil, nil)
	testServer := httptest.NewServer(server.Handler)
	defer testServer.Close()

	response, err := testServer.Client().Get(testServer.URL + "/api/v1/analytics/market?symbol=BTCUSDT&provider=binance&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatalf("request analytics route: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusInternalServerError)
	}
}

type fakeAnalyticsCandleRepository struct {
	candles       []database.MarketCandle
	symbolCandles map[string][]database.MarketCandle
	lookbackLimit int32
}

func (f *fakeAnalyticsCandleRepository) ListByCanonicalSymbol(_ context.Context, symbol, _ string, _ string, from, to pgtype.Timestamptz) ([]database.MarketCandle, error) {
	if f.symbolCandles != nil {
		return append([]database.MarketCandle(nil), f.symbolCandles[symbol]...), nil
	}
	result := make([]database.MarketCandle, 0)
	for _, candle := range f.candles {
		if !candle.ObservedAt.Time.Before(from.Time) && candle.ObservedAt.Time.Before(to.Time) {
			result = append(result, candle)
		}
	}
	return result, nil
}

func (f *fakeAnalyticsCandleRepository) ListBeforeCanonicalSymbol(_ context.Context, _ string, _ string, _ string, before pgtype.Timestamptz, limit int32) ([]database.MarketCandle, error) {
	f.lookbackLimit = limit
	result := make([]database.MarketCandle, 0)
	for _, candle := range f.candles {
		if candle.ObservedAt.Time.Before(before.Time) {
			result = append(result, candle)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ObservedAt.Time.Before(result[right].ObservedAt.Time)
	})
	if len(result) > int(limit) {
		result = result[len(result)-int(limit):]
	}
	return result, nil
}

func analyticsTestCandle(t *testing.T, observedAt time.Time, closeValue int) database.MarketCandle {
	t.Helper()
	return database.MarketCandle{
		ObservedAt: pgtype.Timestamptz{Time: observedAt, Valid: true},
		Close:      httpAnalyticsNumeric(t, closeValue),
	}
}

func httpAnalyticsNumeric(t *testing.T, value int) pgtype.Numeric {
	t.Helper()
	var numeric pgtype.Numeric
	if err := numeric.Scan(fmt.Sprintf("%d", value)); err != nil {
		t.Fatalf("parse test numeric: %v", err)
	}
	return numeric
}
