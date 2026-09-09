package ingestion

import (
	// context carries cancellation into the WebSocket handshake and receive
	// operation so shutdown does not leave a network read blocked forever.
	"context"
	// encoding/json decodes Binance's documented trade-event object.
	"encoding/json"
	// fmt adds provider and operation details to adapter errors.
	"fmt"
	// net/url safely builds the raw stream endpoint from the configured base URL.
	"net/url"
	// strings normalizes Binance symbols and validates the configured endpoint.
	"strings"
	// sync makes Close safe when cancellation and normal shutdown happen together.
	"sync"
	// time converts Binance millisecond timestamps into UTC values and records
	// when the application received the provider message.
	"time"

	"github.com/gorilla/websocket"
)

const (
	// BinanceSpotWebSocketBaseURL is Binance's documented Spot market-stream
	// endpoint. Tests replace it with a local ws:// server.
	BinanceSpotWebSocketBaseURL = "wss://stream.binance.com:9443"
)

// BinanceLiveMarketDataProvider opens Binance Spot raw trade streams.
//
// Binance's provider-specific details stay inside this adapter. The monitor and
// future persistence code only depend on LiveMarketDataProvider and
// LiveMarketStream, so they do not need to know Binance's JSON field names or
// URL format.
type BinanceLiveMarketDataProvider struct {
	dialer  *websocket.Dialer
	baseURL string
}

// NewBinanceLiveMarketDataProvider creates a Binance WebSocket adapter.
//
// A custom dialer is accepted for local tests and future transport settings.
// The base URL is injectable for the same reason: tests should use a local
// server rather than the real Binance network.
func NewBinanceLiveMarketDataProvider(dialer *websocket.Dialer, baseURL string) (*BinanceLiveMarketDataProvider, error) {
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid Binance WebSocket base URL %q", baseURL)
	}
	if parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		return nil, fmt.Errorf("Binance WebSocket base URL must use ws or wss, got %q", parsedURL.Scheme)
	}

	return &BinanceLiveMarketDataProvider{dialer: dialer, baseURL: baseURL}, nil
}

// ProviderName identifies Binance in logs and future live-ingestion metadata.
func (p *BinanceLiveMarketDataProvider) ProviderName() string {
	return string(ProviderBinance)
}

// Capabilities describes this adapter's live trade-stream boundary. Live
// trade events are not requested at a candle interval.
func (p *BinanceLiveMarketDataProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{Live: true}
}

// OpenTradeStream connects to Binance's raw <symbol>@trade stream.
//
// Binance requires stream symbols to be lowercase even though the event payload
// returns the symbol in uppercase. The adapter normalizes the request only for
// the URL and preserves the provider's uppercase symbol in the returned event.
func (p *BinanceLiveMarketDataProvider) OpenTradeStream(ctx context.Context, request LiveMarketStreamRequest) (LiveMarketStream, error) {
	symbol, err := normalizeBinanceStreamSymbol(request.ProviderSymbol)
	if err != nil {
		return nil, err
	}

	endpoint, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Binance WebSocket base URL: %w", err)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/ws/" + symbol + "@trade"
	endpoint.RawQuery = ""
	endpoint.Fragment = ""

	connection, _, err := p.dialer.DialContext(ctx, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("connect Binance trade stream for %s: %w", symbol, err)
	}

	return &binanceLiveMarketStream{
		connection:      connection,
		canonicalSymbol: request.CanonicalSymbol,
	}, nil
}

// binanceLiveMarketStream adapts one Gorilla WebSocket connection to the
// provider-independent LiveMarketStream interface.
type binanceLiveMarketStream struct {
	connection      *websocket.Conn
	canonicalSymbol string
	closeOnce       sync.Once
}

// Receive waits for one Binance message and converts it to a normalized event.
//
// Gorilla's ReadMessage blocks until a frame arrives. Running that read in a
// small goroutine lets context cancellation close the connection immediately,
// which unblocks the read and keeps API shutdown responsive.
func (s *binanceLiveMarketStream) Receive(ctx context.Context) (LiveMarketEvent, error) {
	if err := ctx.Err(); err != nil {
		return LiveMarketEvent{}, err
	}

	type readResult struct {
		payload []byte
		err     error
	}
	resultChannel := make(chan readResult, 1)
	go func() {
		_, payload, err := s.connection.ReadMessage()
		resultChannel <- readResult{payload: payload, err: err}
	}()

	select {
	case <-ctx.Done():
		// Closing the connection causes the blocked ReadMessage call to return.
		// The result channel is buffered so that goroutine can finish safely even
		// though this Receive call is returning through the context branch.
		_ = s.Close()
		return LiveMarketEvent{}, ctx.Err()
	case result := <-resultChannel:
		if result.err != nil {
			return LiveMarketEvent{}, fmt.Errorf("read Binance trade message: %w", result.err)
		}
		event, err := normalizeBinanceTradeMessage(result.payload)
		if err != nil {
			return LiveMarketEvent{}, err
		}
		event.CanonicalSymbol = s.canonicalSymbol
		return event, nil
	}
}

// Close releases the WebSocket connection. sync.Once makes repeated cleanup
// calls safe when both a caller and a canceled Receive attempt to close it.
func (s *binanceLiveMarketStream) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.connection.Close()
	})
	return closeErr
}

// binanceTradeMessage matches only the fields needed by MoneyPlant. Trade ID
// and maker flags are intentionally not included yet because the Phase 2.1
// event contract does not persist them.
type binanceTradeMessage struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	TradeID   int64  `json:"t"`
	Price     string `json:"p"`
	Quantity  string `json:"q"`
	TradeTime int64  `json:"T"`
}

// normalizeBinanceTradeMessage converts Binance JSON into the common event.
func normalizeBinanceTradeMessage(payload []byte) (LiveMarketEvent, error) {
	var message binanceTradeMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return LiveMarketEvent{}, fmt.Errorf("decode Binance trade message: %w", err)
	}
	if message.EventType != "trade" {
		return LiveMarketEvent{}, fmt.Errorf("unsupported Binance WebSocket event type %q", message.EventType)
	}
	if message.EventTime <= 0 || message.TradeTime <= 0 {
		return LiveMarketEvent{}, fmt.Errorf("Binance trade message must contain positive event and trade times")
	}
	if strings.TrimSpace(message.Symbol) == "" {
		return LiveMarketEvent{}, fmt.Errorf("Binance trade message symbol cannot be empty")
	}

	price, err := numericFromString(message.Price)
	if err != nil {
		return LiveMarketEvent{}, fmt.Errorf("parse Binance trade price: %w", err)
	}
	quantity, err := numericFromString(message.Quantity)
	if err != nil {
		return LiveMarketEvent{}, fmt.Errorf("parse Binance trade quantity: %w", err)
	}

	return LiveMarketEvent{
		Provider:         ProviderBinance,
		ProviderSymbol:   strings.ToUpper(message.Symbol),
		EventType:        "trade",
		ObservedAt:       time.UnixMilli(message.TradeTime).UTC(),
		Price:            price,
		Quantity:         quantity,
		SourceReceivedAt: time.Now().UTC(),
	}, nil
}

// normalizeBinanceStreamSymbol validates the small symbol grammar used in a
// raw Binance stream URL. Restricting this to letters and digits avoids placing
// unexpected URL path characters into the endpoint.
func normalizeBinanceStreamSymbol(value string) (string, error) {
	symbol := strings.ToLower(strings.TrimSpace(value))
	if symbol == "" {
		return "", fmt.Errorf("Binance provider symbol cannot be empty")
	}
	for _, character := range symbol {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return "", fmt.Errorf("Binance provider symbol %q contains unsupported character %q", value, character)
		}
	}
	return symbol, nil
}
