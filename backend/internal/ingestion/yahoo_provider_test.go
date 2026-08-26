package ingestion_test

import (
	// context is passed through the provider interface to the test HTTP request.
	"context"
	// net/http validates the request method and writes the fixture response.
	"net/http"
	// net/http/httptest creates a local endpoint without contacting Yahoo Finance.
	"net/http/httptest"
	// testing provides test assertions.
	"testing"
	// time creates the UTC date range used by the chart request.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestYahooMarketDataProviderMapsChart verifies the Yahoo chart-to-candle mapping.
//
// Phase 4.4 update: this test proves that the fallback provider reads unadjusted
// quote arrays, sends the expected daily chart parameters, and returns normalized
// OHLCV values without requiring the real Yahoo Finance service.
func TestYahooMarketDataProviderMapsChart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v8/finance/chart/SBIN.NS" {
			t.Errorf("request path = %s, want /v8/finance/chart/SBIN.NS", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("interval") != "1d" || query.Get("includeAdjustedClose") != "true" {
			t.Errorf("chart query = %v, want daily unadjusted quote request", query)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"chart": {
				"result": [{
					"timestamp": [1785815100],
					"indicators": {
						"quote": [{
							"open": [1047.59997558594],
							"high": [1047.59997558594],
							"low": [1030.90002441406],
							"close": [1042.69995117188],
							"volume": [8544184]
						}]
					}
				}],
				"error": null
			}
		}`))
	}))
	defer server.Close()

	provider, err := ingestion.NewYahooMarketDataProvider(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("create Yahoo provider: %v", err)
	}

	from := time.Unix(1785815100, 0).UTC()
	to := from.Add(24 * time.Hour)
	candles, err := provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderSymbol: "SBIN.NS",
		Interval:       "1d",
		From:           yahooTimestamp(from),
		To:             yahooTimestamp(to),
	})
	if err != nil {
		t.Fatalf("fetch Yahoo candles: %v", err)
	}

	if len(candles) != 1 {
		t.Fatalf("candles = %d, want 1", len(candles))
	}
	candle := candles[0]
	if !candle.ObservedAt.Valid || !candle.ObservedAt.Time.Equal(from) {
		t.Errorf("observed_at = %#v, want %s", candle.ObservedAt, from)
	}
	if !candle.Open.Valid || !candle.High.Valid || !candle.Low.Valid || !candle.Close.Valid || !candle.Volume.Valid {
		t.Fatal("one or more Yahoo OHLCV values are invalid")
	}
	if candle.SourceCloseAt.Valid {
		t.Fatal("Yahoo EOD response should not invent a source close timestamp")
	}
	if candle.QuoteVolume.Valid || candle.TradeCount.Valid || candle.TakerBuyVolume.Valid || candle.TakerBuyQuoteVolume.Valid {
		t.Fatal("Yahoo provider should leave Binance-only metrics NULL")
	}
}

// TestYahooMarketDataProviderRejectsIntradayInterval verifies the Phase 1
// fallback boundary before any network call is made.
func TestYahooMarketDataProviderRejectsIntradayInterval(t *testing.T) {
	provider, err := ingestion.NewYahooMarketDataProvider(http.DefaultClient, "https://example.com")
	if err != nil {
		t.Fatalf("create Yahoo provider: %v", err)
	}

	_, err = provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderSymbol: "SBIN.NS",
		Interval:       "1h",
		From:           yahooTimestamp(time.Unix(1785815100, 0)),
		To:             yahooTimestamp(time.Unix(1785901500, 0)),
	})
	if err == nil {
		t.Fatal("expected unsupported interval error, got nil")
	}
}

// TestYahooMarketDataProviderFailsOverAfterRateLimit verifies controlled
// retry/failover behavior without making any public Yahoo Finance requests.
//
// Phase 4.4 update: the first local endpoint returns HTTP 429 twice with an
// immediate Retry-After value, then the second endpoint returns a valid chart.
func TestYahooMarketDataProviderFailsOverAfterRateLimit(t *testing.T) {
	var primaryCalls int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Edge: Too Many Requests"))
	}))
	defer primary.Close()

	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"chart": {
				"result": [{
					"timestamp": [1785815100],
					"indicators": {"quote": [{
						"open": [1043], "high": [1055], "low": [1040.3],
						"close": [1055], "volume": [8707119]
					}]}
				}],
				"error": null
			}
		}`))
	}))
	defer secondary.Close()

	provider, err := ingestion.NewYahooMarketDataProviderWithFallbacks(
		primary.Client(),
		primary.URL,
		secondary.URL,
	)
	if err != nil {
		t.Fatalf("create Yahoo provider: %v", err)
	}

	from := time.Unix(1785815100, 0).UTC()
	candles, err := provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderSymbol: "SBIN.NS",
		Interval:       "1d",
		From:           yahooTimestamp(from),
		To:             yahooTimestamp(from.Add(24 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("fetch Yahoo candles through fallback: %v", err)
	}
	if primaryCalls != 2 {
		t.Fatalf("primary calls = %d, want one initial request plus one retry", primaryCalls)
	}
	if len(candles) != 1 {
		t.Fatalf("candles = %d, want 1 from fallback endpoint", len(candles))
	}
}

// yahooTimestamp keeps request construction readable while producing the exact
// pgtype representation expected by HistoricalCandleRequest.
func yahooTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
