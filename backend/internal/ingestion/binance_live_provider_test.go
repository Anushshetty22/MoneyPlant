package ingestion_test

import (
	// context controls the adapter receive lifecycle.
	"context"
	// encoding/json writes a Binance-shaped trade message from the local server.
	"encoding/json"
	// net/http creates the local HTTP handler used by the WebSocket upgrader.
	"net/http"
	// net/http/httptest provides an in-process server without external network access.
	"net/http/httptest"
	// strings converts the test server's http URL into a ws URL.
	"strings"
	// testing provides assertions.
	"testing"
	// time checks Binance millisecond timestamps become UTC values.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/gorilla/websocket"
)

// TestBinanceLiveMarketDataProviderMapsTrade verifies the complete provider
// boundary using a local WebSocket server. No Binance credentials or network
// access are required.
func TestBinanceLiveMarketDataProviderMapsTrade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ws/btcusdt@trade" {
			t.Errorf("WebSocket path = %q, want /ws/btcusdt@trade", request.URL.Path)
		}

		upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Errorf("upgrade WebSocket: %v", err)
			return
		}
		defer connection.Close()

		message := map[string]any{
			"e": "trade",
			"E": int64(1786528800123),
			"s": "BTCUSDT",
			"t": int64(12345),
			"p": "64323.61000000",
			"q": "0.01000000",
			"T": int64(1786528800000),
			"m": false,
			"M": true,
		}
		if err := connection.WriteJSON(message); err != nil {
			t.Errorf("write trade message: %v", err)
		}
	}))
	defer server.Close()

	provider, err := ingestion.NewBinanceLiveMarketDataProvider(nil, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatalf("create Binance live provider: %v", err)
	}
	stream, err := provider.OpenTradeStream(context.Background(), ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTCUSDT"})
	if err != nil {
		t.Fatalf("open Binance trade stream: %v", err)
	}
	defer stream.Close()

	event, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive Binance trade event: %v", err)
	}

	if event.ProviderSymbol != "BTCUSDT" || event.EventType != "trade" {
		t.Fatalf("event identity = %#v, want BTCUSDT trade", event)
	}
	if !event.ObservedAt.Equal(time.UnixMilli(1786528800000).UTC()) {
		t.Errorf("observed_at = %s, want provider trade time", event.ObservedAt)
	}
	if !event.Price.Valid || !event.Quantity.Valid {
		t.Fatal("price and quantity should be valid exact decimals")
	}
	encodedPrice, err := json.Marshal(event.Price)
	if err != nil {
		t.Fatalf("marshal price: %v", err)
	}
	if string(encodedPrice) != "64323.61000000" {
		t.Errorf("price = %s, want 64323.61000000", encodedPrice)
	}
}

// TestBinanceLiveMarketDataProviderRejectsUnsupportedMessage confirms that the
// adapter does not silently treat a different Binance stream message as a trade.
func TestBinanceLiveMarketDataProviderRejectsUnsupportedMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Errorf("upgrade WebSocket: %v", err)
			return
		}
		defer connection.Close()
		_ = connection.WriteJSON(map[string]any{"e": "kline"})
	}))
	defer server.Close()

	provider, err := ingestion.NewBinanceLiveMarketDataProvider(nil, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatalf("create Binance live provider: %v", err)
	}
	stream, err := provider.OpenTradeStream(context.Background(), ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTCUSDT"})
	if err != nil {
		t.Fatalf("open Binance trade stream: %v", err)
	}
	defer stream.Close()

	if _, err := stream.Receive(context.Background()); err == nil {
		t.Fatal("expected unsupported event type error, got nil")
	}
}

// TestBinanceLiveMarketDataProviderRejectsUnsafeSymbol verifies URL path input
// is validated before any network connection is attempted.
func TestBinanceLiveMarketDataProviderRejectsUnsafeSymbol(t *testing.T) {
	provider, err := ingestion.NewBinanceLiveMarketDataProvider(nil, "wss://example.com")
	if err != nil {
		t.Fatalf("create Binance live provider: %v", err)
	}

	if _, err := provider.OpenTradeStream(context.Background(), ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTC/USDT"}); err == nil {
		t.Fatal("expected unsafe symbol error, got nil")
	}
}
