package ingestion_test

import (
	// context is passed into the provider request to exercise the real interface.
	"context"
	// encoding/json creates a 1,000-row response for the pagination test.
	"encoding/json"
	// net/http validates the request method and lets the test server return a fixture response.
	"net/http"
	// net/http/httptest creates a local HTTP endpoint without calling Binance's network.
	"net/http/httptest"
	// testing provides assertions and test lifecycle management.
	"testing"
	// time creates the UTC request window used by the adapter.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestBinanceMarketDataProviderMapsKline verifies the complete adapter boundary.
//
// Phase 4.2 update: this test proves that a Binance-shaped 12-field response is
// requested with the expected query parameters and converted into the normalized
// candle model without using the real Binance service.
func TestBinanceMarketDataProviderMapsKline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v3/klines" {
			t.Errorf("request path = %s, want /api/v3/klines", r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("symbol") != "BTCUSDT" {
			t.Errorf("symbol = %q, want BTCUSDT", query.Get("symbol"))
		}
		if query.Get("interval") != "1d" {
			t.Errorf("interval = %q, want 1d", query.Get("interval"))
		}
		if query.Get("limit") != "1000" {
			t.Errorf("limit = %q, want 1000", query.Get("limit"))
		}
		if query.Get("timeZone") != "0" {
			t.Errorf("timeZone = %q, want 0", query.Get("timeZone"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			[1785974400000,"64665.24000000","64999.00000000","64172.00000000","64323.61000000","9864.43284000",1786060799999,"637366646.71670770",2087951,"4666.15261000","301530716.38684180","0"]
		]`))
	}))
	defer server.Close()

	provider, err := ingestion.NewBinanceMarketDataProvider(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("create Binance provider: %v", err)
	}

	from := time.UnixMilli(1785974400000).UTC()
	to := time.UnixMilli(1786060800000).UTC()
	candles, err := provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderSymbol: "BTCUSDT",
		Interval:       "1d",
		From:           binanceTimestamp(from),
		To:             binanceTimestamp(to),
	})
	if err != nil {
		t.Fatalf("fetch Binance candles: %v", err)
	}

	if len(candles) != 1 {
		t.Fatalf("candles = %d, want 1", len(candles))
	}
	candle := candles[0]
	if candle.Interval != "1d" {
		t.Errorf("interval = %q, want 1d", candle.Interval)
	}
	if !candle.ObservedAt.Valid || !candle.ObservedAt.Time.Equal(from) {
		t.Errorf("observed_at = %#v, want %s", candle.ObservedAt, from)
	}
	if !candle.SourceCloseAt.Valid || !candle.SourceCloseAt.Time.Equal(time.UnixMilli(1786060799999).UTC()) {
		t.Errorf("source_close_at = %#v, want Binance close timestamp", candle.SourceCloseAt)
	}
	if !candle.Open.Valid || !candle.High.Valid || !candle.Low.Valid || !candle.Close.Valid || !candle.Volume.Valid {
		t.Fatal("one or more required OHLCV numerics are invalid")
	}
	if !candle.QuoteVolume.Valid || !candle.TakerBuyVolume.Valid || !candle.TakerBuyQuoteVolume.Valid {
		t.Fatal("Binance-specific numeric fields were not preserved")
	}
	if !candle.TradeCount.Valid || candle.TradeCount.Int64 != 2087951 {
		t.Errorf("trade_count = %#v, want 2087951", candle.TradeCount)
	}
}

// TestBinanceMarketDataProviderRejectsUnsupportedInterval verifies that an
// invalid Phase 1 interval is rejected before an HTTP request is attempted.
func TestBinanceMarketDataProviderRejectsUnsupportedInterval(t *testing.T) {
	provider, err := ingestion.NewBinanceMarketDataProvider(http.DefaultClient, "https://example.com")
	if err != nil {
		t.Fatalf("create Binance provider: %v", err)
	}

	_, err = provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderSymbol: "BTCUSDT",
		Interval:       "3m",
		From:           binanceTimestamp(time.UnixMilli(1785974400000)),
		To:             binanceTimestamp(time.UnixMilli(1786060800000)),
	})
	if err == nil {
		t.Fatal("expected unsupported interval error, got nil")
	}
}

// TestBinanceMarketDataProviderPaginatesLargeWindows verifies that a full
// provider page advances the request window instead of silently truncating data.
//
// Phase 5.3 update: the local server returns 1,000 candles on the first request
// and one candle on the second request. The test confirms the adapter returns
// all 1,001 rows and makes the expected two-page workflow without Binance access.
func TestBinanceMarketDataProviderPaginatesLargeWindows(t *testing.T) {
	const candleMilliseconds int64 = 24 * 60 * 60 * 1000
	baseOpen := int64(1785974400000)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		rowCount := 1
		firstOpen := baseOpen + int64(requestCount-1)*1000*candleMilliseconds
		if requestCount == 1 {
			rowCount = 1000
		}

		rows := make([][]any, 0, rowCount)
		for index := 0; index < rowCount; index++ {
			openTime := firstOpen + int64(index)*candleMilliseconds
			rows = append(rows, []any{
				openTime,
				"100.00000000", "105.00000000", "95.00000000", "102.00000000", "1000.00000000",
				openTime + candleMilliseconds - 1, "102000.00000000", 10,
				"500.00000000", "51000.00000000", "0",
			})
		}

		body, err := json.Marshal(rows)
		if err != nil {
			t.Errorf("marshal pagination response: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	provider, err := ingestion.NewBinanceMarketDataProvider(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("create Binance provider: %v", err)
	}

	from := time.UnixMilli(baseOpen).UTC()
	to := from.Add(1001 * 24 * time.Hour)
	candles, err := provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderSymbol: "BTCUSDT",
		Interval:       "1d",
		From:           binanceTimestamp(from),
		To:             binanceTimestamp(to),
	})
	if err != nil {
		t.Fatalf("fetch paginated Binance candles: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
	if len(candles) != 1001 {
		t.Fatalf("candles = %d, want 1001", len(candles))
	}
}

// binanceTimestamp keeps test request construction readable while producing the exact
// pgtype timestamp representation expected by HistoricalCandleRequest.
func binanceTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
