package ingestion_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/gorilla/websocket"
)

func TestAngelOneLiveProviderSubscribesSixTokensAndMapsEvents(t *testing.T) {
	subscriptions := sixAngelOneSubscriptions()
	server, connectionCount := newAngelOneTestWebSocketServer(t, subscriptions, false)
	defer server.Close()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/rest/auth/angelbroking/user/v1/loginByPassword" {
			return jsonResponse(http.StatusNotFound, `{"status":false,"message":"not found","errorcode":"","data":null}`), nil
		}
		return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":{"jwtToken":"jwt","refreshToken":"refresh","feedToken":"feed"}}`), nil
	})}
	authenticator, err := ingestion.NewAngelOneAuthenticator(client, "https://auth.test", ingestion.AngelOneCredentials{
		APIKey: "test-api-key", ClientCode: "TEST123", Password: "test-pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
	})
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	session, err := authenticator.Login(context.Background())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	provider, err := ingestion.NewAngelOneLiveMarketDataProvider(server.Dialer(), authenticator, session, server.URL)
	if err != nil {
		t.Fatalf("create live provider: %v", err)
	}
	stream, err := provider.OpenMultiplexedTradeStream(context.Background(), subscriptions)
	if err != nil {
		t.Fatalf("open multiplexed stream: %v", err)
	}
	defer stream.Close()

	seen := make(map[string]bool)
	for range subscriptions {
		event, err := stream.Receive(context.Background())
		if err != nil {
			t.Fatalf("receive Angel One event: %v", err)
		}
		seen[event.CanonicalSymbol] = true
		if event.Provider != ingestion.ProviderAngelOne || event.EventType != "trade" || event.Quantity.Int.String() == "" {
			t.Fatalf("normalized event = %#v", event)
		}
	}
	if len(seen) != len(subscriptions) {
		t.Fatalf("canonical events = %d, want %d", len(seen), len(subscriptions))
	}
	if connectionCount.Load() != 1 {
		t.Fatalf("WebSocket connection count = %d, want 1 multiplexed connection", connectionCount.Load())
	}
}

func TestAngelOneLiveProviderReconnectsThroughSharedWrapper(t *testing.T) {
	subscriptions := sixAngelOneSubscriptions()
	server, connectionCount := newAngelOneTestWebSocketServer(t, subscriptions, true)
	defer server.Close()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":{"jwtToken":"jwt","refreshToken":"refresh","feedToken":"feed"}}`), nil
	})}
	authenticator, err := ingestion.NewAngelOneAuthenticator(client, "https://auth.test", ingestion.AngelOneCredentials{
		APIKey: "test-api-key", ClientCode: "TEST123", Password: "test-pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
	})
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	session, err := authenticator.Login(context.Background())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	provider, err := ingestion.NewAngelOneLiveMarketDataProvider(server.Dialer(), authenticator, session, server.URL)
	if err != nil {
		t.Fatalf("create live provider: %v", err)
	}
	multiplexedProvider, err := provider.MultiplexedProvider(subscriptions)
	if err != nil {
		t.Fatalf("create multiplexed provider: %v", err)
	}
	policy := ingestion.DefaultLiveReconnectPolicy()
	policy.MaxRetries = 1
	policy.InitialBackoff = time.Millisecond
	policy.MaxBackoff = time.Millisecond
	policy.Sleep = func(context.Context, time.Duration) error { return nil }
	stream, err := ingestion.NewReconnectingLiveMarketStream(multiplexedProvider, ingestion.LiveMarketStreamRequest{ProviderSymbol: "multiplexed"}, policy)
	if err != nil {
		t.Fatalf("create reconnecting stream: %v", err)
	}
	defer stream.Close()

	first, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive first event: %v", err)
	}
	second, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive event after reconnect: %v", err)
	}
	if first.CanonicalSymbol != "NIFTY50" || second.CanonicalSymbol != "NIFTY50" {
		t.Fatalf("reconnected events = %#v / %#v", first, second)
	}
	if connectionCount.Load() != 2 {
		t.Fatalf("WebSocket connection count = %d, want 2 after reconnect", connectionCount.Load())
	}
}

func TestAngelOneLiveProviderReauthenticatesAfterWebSocketExpiry(t *testing.T) {
	subscriptions := sixAngelOneSubscriptions()
	server, handshakeCount := newAngelOneReauthWebSocketServer(t, subscriptions)
	defer server.Close()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/rest/auth/angelbroking/user/v1/loginByPassword":
			return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":{"jwtToken":"jwt-old","refreshToken":"refresh-old","feedToken":"feed-old"}}`), nil
		case "/rest/auth/angelbroking/jwt/v1/generateTokens":
			if request.Header.Get("Authorization") != "Bearer jwt-old" {
				t.Errorf("refresh Authorization = %q, want old JWT", request.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":{"jwtToken":"jwt-new","refreshToken":"refresh-new","feedToken":"feed-new"}}`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{"status":false,"message":"not found","errorcode":"","data":null}`), nil
		}
	})}
	authenticator, err := ingestion.NewAngelOneAuthenticator(client, "https://auth.test", ingestion.AngelOneCredentials{
		APIKey: "test-api-key", ClientCode: "TEST123", Password: "test-pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
	})
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	session, err := authenticator.Login(context.Background())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	provider, err := ingestion.NewAngelOneLiveMarketDataProvider(server.Dialer(), authenticator, session, server.URL)
	if err != nil {
		t.Fatalf("create live provider: %v", err)
	}
	stream, err := provider.OpenMultiplexedTradeStream(context.Background(), subscriptions)
	if err != nil {
		t.Fatalf("open stream after re-authentication: %v", err)
	}
	defer stream.Close()

	event, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive event after re-authentication: %v", err)
	}
	if event.CanonicalSymbol != "NIFTY50" {
		t.Fatalf("event after re-authentication = %#v", event)
	}
	if handshakeCount.Load() != 2 {
		t.Fatalf("WebSocket handshake count = %d, want failed old session plus new session", handshakeCount.Load())
	}
}

func sixAngelOneSubscriptions() []ingestion.AngelOneLiveSubscription {
	return []ingestion.AngelOneLiveSubscription{
		{CanonicalSymbol: "NIFTY50", ProviderSymbol: "Nifty 50", ProviderInstrumentID: "99926000", ExchangeType: 1},
		{CanonicalSymbol: "SBIN", ProviderSymbol: "SBIN-EQ", ProviderInstrumentID: "3045", ExchangeType: 1},
		{CanonicalSymbol: "RELIANCE", ProviderSymbol: "RELIANCE-EQ", ProviderInstrumentID: "2885", ExchangeType: 1},
		{CanonicalSymbol: "TCS", ProviderSymbol: "TCS-EQ", ProviderInstrumentID: "11536", ExchangeType: 1},
		{CanonicalSymbol: "INFY", ProviderSymbol: "INFY-EQ", ProviderInstrumentID: "1594", ExchangeType: 1},
		{CanonicalSymbol: "HDFCBANK", ProviderSymbol: "HDFCBANK-EQ", ProviderInstrumentID: "1333", ExchangeType: 1},
	}
}

func newAngelOneTestWebSocketServer(
	t *testing.T,
	subscriptions []ingestion.AngelOneLiveSubscription,
	closeAfterFirst bool,
) (*angelOneTestWebSocketServer, *atomic.Int32) {
	t.Helper()
	var connectionCount atomic.Int32
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer jwt" || request.Header.Get("x-api-key") != "test-api-key" || request.Header.Get("x-client-code") != "TEST123" || request.Header.Get("x-feed-token") != "feed" {
			t.Errorf("missing WebSocket auth headers: %#v", request.Header)
		}
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Errorf("upgrade WebSocket: %v", err)
			return
		}
		defer connection.Close()
		count := connectionCount.Add(1)
		messageType, payload, err := connection.ReadMessage()
		if err != nil || messageType != websocket.TextMessage {
			t.Errorf("read subscription message: type=%d err=%v", messageType, err)
			return
		}
		var subscriptionRequest struct {
			Action int `json:"action"`
			Params struct {
				Mode      byte `json:"mode"`
				TokenList []struct {
					ExchangeType byte     `json:"exchangeType"`
					Tokens       []string `json:"tokens"`
				} `json:"tokenList"`
			} `json:"params"`
		}
		if err := json.Unmarshal(payload, &subscriptionRequest); err != nil {
			t.Errorf("decode subscription: %v", err)
			return
		}
		if subscriptionRequest.Action != 1 || subscriptionRequest.Params.Mode != 2 || len(subscriptionRequest.Params.TokenList) != 1 || len(subscriptionRequest.Params.TokenList[0].Tokens) != len(subscriptions) {
			t.Errorf("subscription request = %#v", subscriptionRequest)
			return
		}
		for index, subscription := range subscriptions {
			if subscriptionRequest.Params.TokenList[0].Tokens[index] != subscription.ProviderInstrumentID {
				t.Errorf("subscription token[%d] = %q, want %q", index, subscriptionRequest.Params.TokenList[0].Tokens[index], subscription.ProviderInstrumentID)
			}
		}
		if closeAfterFirst {
			_ = connection.WriteMessage(websocket.BinaryMessage, angelOnePacket(ingestion.AngelOneSubscriptionModeQuote, 1, subscriptions[0].ProviderInstrumentID, 1788241500123+int64(count), 1234567, 7, 123))
			return
		}
		// A malformed packet must be ignored while the same socket continues
		// delivering valid events for every subscribed instrument.
		_ = connection.WriteMessage(websocket.BinaryMessage, []byte{ingestion.AngelOneSubscriptionModeQuote, 1})
		for index, subscription := range subscriptions {
			_ = connection.WriteMessage(websocket.BinaryMessage, angelOnePacket(ingestion.AngelOneSubscriptionModeQuote, 1, subscription.ProviderInstrumentID, 1788241500123+int64(index), 1234567+int64(index), int64(index+1), 123))
		}
	})
	listener := newAngelOnePipeListener()
	httpServer := &http.Server{Handler: handler}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	server := &angelOneTestWebSocketServer{
		URL:      "ws://angel.test",
		server:   httpServer,
		listener: listener,
	}
	t.Cleanup(server.Close)
	return server, &connectionCount
}

func newAngelOneReauthWebSocketServer(
	t *testing.T,
	subscriptions []ingestion.AngelOneLiveSubscription,
) (*angelOneTestWebSocketServer, *atomic.Int32) {
	t.Helper()
	var handshakeCount atomic.Int32
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if handshakeCount.Add(1) == 1 {
			http.Error(response, "expired", http.StatusUnauthorized)
			return
		}
		if request.Header.Get("Authorization") != "Bearer jwt-new" || request.Header.Get("x-feed-token") != "feed-new" {
			t.Errorf("reauthenticated WebSocket headers: Authorization=%q feed=%q", request.Header.Get("Authorization"), request.Header.Get("x-feed-token"))
		}
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Errorf("upgrade reauthenticated WebSocket: %v", err)
			return
		}
		defer connection.Close()
		if messageType, _, err := connection.ReadMessage(); err != nil || messageType != websocket.TextMessage {
			t.Errorf("read reauthenticated subscription: type=%d err=%v", messageType, err)
			return
		}
		_ = connection.WriteMessage(websocket.BinaryMessage, angelOnePacket(
			ingestion.AngelOneSubscriptionModeQuote,
			1,
			subscriptions[0].ProviderInstrumentID,
			1788241500123,
			1234567,
			7,
			123,
		))
	})
	listener := newAngelOnePipeListener()
	httpServer := &http.Server{Handler: handler}
	go func() { _ = httpServer.Serve(listener) }()
	server := &angelOneTestWebSocketServer{URL: "ws://angel.test", server: httpServer, listener: listener}
	t.Cleanup(server.Close)
	return server, &handshakeCount
}

type angelOneTestWebSocketServer struct {
	URL      string
	server   *http.Server
	listener *angelOnePipeListener
}

func (s *angelOneTestWebSocketServer) Close() {
	_ = s.listener.Close()
	_ = s.server.Close()
}

func (s *angelOneTestWebSocketServer) Dialer() *websocket.Dialer {
	return &websocket.Dialer{
		NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			client, server := net.Pipe()
			select {
			case s.listener.connections <- server:
				return client, nil
			case <-ctx.Done():
				_ = client.Close()
				_ = server.Close()
				return nil, ctx.Err()
			}
		},
	}
}

type angelOnePipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func newAngelOnePipeListener() *angelOnePipeListener {
	return &angelOnePipeListener{
		connections: make(chan net.Conn),
		closed:      make(chan struct{}),
	}
}

func (l *angelOnePipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		if connection == nil {
			return nil, net.ErrClosed
		}
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *angelOnePipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *angelOnePipeListener) Addr() net.Addr { return angelOnePipeAddr("angel-one-test") }

type angelOnePipeAddr string

func (angelOnePipeAddr) Network() string { return "pipe" }

func (address angelOnePipeAddr) String() string { return string(address) }
