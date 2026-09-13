package ingestion

import (
	// context controls dialing, receiving, heartbeat shutdown, and re-authentication.
	"context"
	// encoding/json encodes subscriptions and decodes provider error messages.
	"encoding/json"
	// errors distinguishes context cancellation from transport and auth failures.
	"errors"
	// fmt creates safe connection and subscription errors.
	"fmt"
	// net/http supplies WebSocket handshake headers and response status handling.
	"net/http"
	// net/url validates the injectable WebSocket endpoint.
	"net/url"
	// strings normalizes tokens, text control messages, and safe provider errors.
	"strings"
	// sync protects session replacement, WebSocket writes, and one-time shutdown.
	"sync"
	// time controls heartbeats and records local receive time.
	"time"

	"github.com/gorilla/websocket"
)

const (
	// AngelOneSmartStreamURL is the documented SmartAPI WebSocket 2.0 endpoint.
	AngelOneSmartStreamURL = "wss://smartapisocket.angelone.in/smart-stream"
	// angelOneQuoteMode supplies last-traded quantity required by LiveMarketEvent.
	angelOneQuoteMode byte = 2
	// SmartAPI requires a client heartbeat every 30 seconds.
	angelOneDefaultHeartbeatInterval      = 30 * time.Second
	angelOneNSECashExchangeType      byte = 1
	angelOneSubscriptionCorrelation       = "mp00000001"
)

// AngelOneLiveSubscription connects one canonical MoneyPlant identity to one
// provider token on the multiplexed SmartAPI connection.
type AngelOneLiveSubscription struct {
	CanonicalSymbol      string
	ProviderSymbol       string
	ProviderInstrumentID string
	ExchangeType         byte
	InstrumentSourceID   int64
}

func (s AngelOneLiveSubscription) validate() error {
	if strings.TrimSpace(s.CanonicalSymbol) == "" {
		return fmt.Errorf("Angel One live canonical symbol cannot be empty")
	}
	if strings.TrimSpace(s.ProviderSymbol) == "" {
		return fmt.Errorf("Angel One live provider symbol cannot be empty")
	}
	if strings.TrimSpace(s.ProviderInstrumentID) == "" {
		return fmt.Errorf("Angel One live provider instrument ID cannot be empty")
	}
	if !validAngelOneExchangeType(s.ExchangeType) {
		return fmt.Errorf("unsupported Angel One live exchange type %d", s.ExchangeType)
	}
	return nil
}

// AngelOneLiveMarketDataProvider opens authenticated multiplexed SmartAPI
// streams. It owns only in-memory session state; credentials and provider
// tokens are never persisted or included in errors.
type AngelOneLiveMarketDataProvider struct {
	dialer            *websocket.Dialer
	authenticator     *AngelOneAuthenticator
	baseURL           string
	heartbeatInterval time.Duration
	sessionMu         sync.Mutex
	session           AngelOneSession
}

// NewAngelOneLiveMarketDataProvider creates a production Angel One live
// provider. The supplied session normally comes from Authenticator.Login.
func NewAngelOneLiveMarketDataProvider(
	dialer *websocket.Dialer,
	authenticator *AngelOneAuthenticator,
	session AngelOneSession,
	baseURL string,
) (*AngelOneLiveMarketDataProvider, error) {
	return newAngelOneLiveMarketDataProvider(dialer, authenticator, session, baseURL, angelOneDefaultHeartbeatInterval)
}

func newAngelOneLiveMarketDataProvider(
	dialer *websocket.Dialer,
	authenticator *AngelOneAuthenticator,
	session AngelOneSession,
	baseURL string,
	heartbeatInterval time.Duration,
) (*AngelOneLiveMarketDataProvider, error) {
	if authenticator == nil {
		return nil, fmt.Errorf("Angel One authenticator cannot be nil")
	}
	if heartbeatInterval <= 0 {
		return nil, fmt.Errorf("Angel One heartbeat interval must be positive")
	}
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = AngelOneSmartStreamURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		return nil, fmt.Errorf("invalid Angel One WebSocket base URL")
	}
	return &AngelOneLiveMarketDataProvider{
		dialer:            dialer,
		authenticator:     authenticator,
		baseURL:           baseURL,
		heartbeatInterval: heartbeatInterval,
		session:           session,
	}, nil
}

func (p *AngelOneLiveMarketDataProvider) ProviderName() string {
	return string(ProviderAngelOne)
}

func (p *AngelOneLiveMarketDataProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{Live: true}
}

// OpenTradeStream preserves the common one-request provider interface. The
// provider-specific multiplexed method below is used when several instruments
// share one SmartAPI connection.
func (p *AngelOneLiveMarketDataProvider) OpenTradeStream(ctx context.Context, request LiveMarketStreamRequest) (LiveMarketStream, error) {
	return p.OpenMultiplexedTradeStream(ctx, []AngelOneLiveSubscription{{
		CanonicalSymbol:      request.CanonicalSymbol,
		ProviderSymbol:       request.ProviderSymbol,
		ProviderInstrumentID: request.ProviderInstrumentID,
		ExchangeType:         angelOneNSECashExchangeType,
	}})
}

// OpenMultiplexedTradeStream opens one connection and subscribes all supplied
// tokens in one Quote-mode request. The stream maps each returned token back to
// its canonical identity before producing a normalized event.
func (p *AngelOneLiveMarketDataProvider) OpenMultiplexedTradeStream(
	ctx context.Context,
	subscriptions []AngelOneLiveSubscription,
) (LiveMarketStream, error) {
	if len(subscriptions) == 0 {
		return nil, fmt.Errorf("Angel One live subscriptions cannot be empty")
	}
	subscriptions = append([]AngelOneLiveSubscription(nil), subscriptions...)
	tokenMap := make(map[string]AngelOneLiveSubscription, len(subscriptions))
	for index, subscription := range subscriptions {
		if err := subscription.validate(); err != nil {
			return nil, fmt.Errorf("validate Angel One live subscription %d: %w", index, err)
		}
		token := strings.TrimSpace(subscription.ProviderInstrumentID)
		if _, exists := tokenMap[token]; exists {
			return nil, fmt.Errorf("duplicate Angel One live provider token %q", token)
		}
		tokenMap[token] = subscription
		subscriptions[index].ProviderInstrumentID = token
	}

	session, err := p.currentSession(ctx)
	if err != nil {
		return nil, err
	}
	connection, err := p.dial(ctx, session)
	if err != nil {
		if !isAngelOneWebSocketAuthFailure(err) {
			return nil, err
		}
		session, err = p.reauthenticate(ctx, session)
		if err != nil {
			return nil, err
		}
		connection, err = p.dial(ctx, session)
		if err != nil {
			return nil, err
		}
	}

	stream := &angelOneLiveMarketStream{
		connection:        connection,
		tokenMap:          tokenMap,
		heartbeatInterval: p.heartbeatInterval,
	}
	if err := stream.subscribe(subscriptions); err != nil {
		_ = stream.Close()
		return nil, err
	}
	stream.startHeartbeat()
	return stream, nil
}

// MultiplexedProvider adapts one fixed subscription set to the common provider
// interface so ReconnectingLiveMarketStream can reopen the same socket after a
// disconnect.
func (p *AngelOneLiveMarketDataProvider) MultiplexedProvider(subscriptions []AngelOneLiveSubscription) (LiveMarketDataProvider, error) {
	if len(subscriptions) == 0 {
		return nil, fmt.Errorf("Angel One live subscriptions cannot be empty")
	}
	copySubscriptions := append([]AngelOneLiveSubscription(nil), subscriptions...)
	for index, subscription := range copySubscriptions {
		if err := subscription.validate(); err != nil {
			return nil, fmt.Errorf("validate Angel One live subscription %d: %w", index, err)
		}
	}
	return &angelOneMultiplexedProvider{provider: p, subscriptions: copySubscriptions}, nil
}

type angelOneMultiplexedProvider struct {
	provider      *AngelOneLiveMarketDataProvider
	subscriptions []AngelOneLiveSubscription
}

func (p *angelOneMultiplexedProvider) ProviderName() string { return string(ProviderAngelOne) }

func (p *angelOneMultiplexedProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{Live: true}
}

func (p *angelOneMultiplexedProvider) OpenTradeStream(ctx context.Context, _ LiveMarketStreamRequest) (LiveMarketStream, error) {
	return p.provider.OpenMultiplexedTradeStream(ctx, p.subscriptions)
}

func (p *AngelOneLiveMarketDataProvider) currentSession(ctx context.Context) (AngelOneSession, error) {
	p.sessionMu.Lock()
	session := p.session
	p.sessionMu.Unlock()
	if strings.TrimSpace(session.JWTToken()) != "" && strings.TrimSpace(session.FeedToken()) != "" {
		return session, nil
	}
	return p.login(ctx)
}

func (p *AngelOneLiveMarketDataProvider) login(ctx context.Context) (AngelOneSession, error) {
	session, err := p.authenticator.Login(ctx)
	if err != nil {
		return AngelOneSession{}, fmt.Errorf("Angel One live authentication failed: %w", err)
	}
	p.sessionMu.Lock()
	p.session = session
	p.sessionMu.Unlock()
	return session, nil
}

func (p *AngelOneLiveMarketDataProvider) reauthenticate(ctx context.Context, session AngelOneSession) (AngelOneSession, error) {
	if strings.TrimSpace(session.RefreshToken()) != "" {
		refreshed, err := p.authenticator.Refresh(ctx, session)
		if err == nil {
			p.sessionMu.Lock()
			p.session = refreshed
			p.sessionMu.Unlock()
			return refreshed, nil
		}
		if !IsAngelOneSessionExpired(err) {
			return AngelOneSession{}, fmt.Errorf("Angel One live session refresh failed: %w", err)
		}
	}
	return p.login(ctx)
}

func (p *AngelOneLiveMarketDataProvider) dial(ctx context.Context, session AngelOneSession) (*websocket.Conn, error) {
	headers := http.Header{}
	headers.Set("Authorization", normalizeBearerToken(session.JWTToken()))
	headers.Set("x-api-key", p.authenticator.credentials.APIKey)
	headers.Set("x-client-code", p.authenticator.credentials.ClientCode)
	headers.Set("x-feed-token", session.FeedToken())
	connection, response, err := p.dialer.DialContext(ctx, p.baseURL, headers)
	if err != nil {
		if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
			return nil, &angelOneWebSocketAuthError{statusCode: response.StatusCode}
		}
		return nil, fmt.Errorf("connect Angel One live WebSocket: %w", err)
	}
	return connection, nil
}

type angelOneWebSocketAuthError struct{ statusCode int }

func (e *angelOneWebSocketAuthError) Error() string {
	return fmt.Sprintf("Angel One live WebSocket authentication failed with HTTP %d", e.statusCode)
}

func isAngelOneWebSocketAuthFailure(err error) bool {
	var authErr *angelOneWebSocketAuthError
	return errors.As(err, &authErr)
}

type angelOneSubscriptionRequest struct {
	CorrelationID string                         `json:"correlationID"`
	Action        int                            `json:"action"`
	Params        angelOneSubscriptionParameters `json:"params"`
}

type angelOneSubscriptionParameters struct {
	Mode      byte                 `json:"mode"`
	TokenList []angelOneTokenGroup `json:"tokenList"`
}

type angelOneTokenGroup struct {
	ExchangeType byte     `json:"exchangeType"`
	Tokens       []string `json:"tokens"`
}

func buildAngelOneSubscriptionRequest(subscriptions []AngelOneLiveSubscription) angelOneSubscriptionRequest {
	groups := make(map[byte][]string)
	order := make([]byte, 0)
	for _, subscription := range subscriptions {
		if _, exists := groups[subscription.ExchangeType]; !exists {
			order = append(order, subscription.ExchangeType)
		}
		groups[subscription.ExchangeType] = append(groups[subscription.ExchangeType], subscription.ProviderInstrumentID)
	}
	tokenList := make([]angelOneTokenGroup, 0, len(order))
	for _, exchangeType := range order {
		tokenList = append(tokenList, angelOneTokenGroup{ExchangeType: exchangeType, Tokens: groups[exchangeType]})
	}
	return angelOneSubscriptionRequest{
		CorrelationID: angelOneSubscriptionCorrelation,
		Action:        1,
		Params:        angelOneSubscriptionParameters{Mode: angelOneQuoteMode, TokenList: tokenList},
	}
}

type angelOneLiveMarketStream struct {
	connection        *websocket.Conn
	tokenMap          map[string]AngelOneLiveSubscription
	heartbeatInterval time.Duration
	writeMu           sync.Mutex
	closeOnce         sync.Once
	heartbeatDone     chan struct{}
}

func (s *angelOneLiveMarketStream) subscribe(subscriptions []AngelOneLiveSubscription) error {
	payload, err := json.Marshal(buildAngelOneSubscriptionRequest(subscriptions))
	if err != nil {
		return fmt.Errorf("marshal Angel One subscription: %w", err)
	}
	s.writeMu.Lock()
	err = s.connection.WriteMessage(websocket.TextMessage, payload)
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("send Angel One subscription: %w", err)
	}
	return nil
}

func (s *angelOneLiveMarketStream) startHeartbeat() {
	s.heartbeatDone = make(chan struct{})
	go func() {
		ticker := time.NewTicker(s.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.writeMu.Lock()
				err := s.connection.WriteMessage(websocket.TextMessage, []byte("ping"))
				s.writeMu.Unlock()
				if err != nil {
					_ = s.Close()
					return
				}
			case <-s.heartbeatDone:
				return
			}
		}
	}()
}

func (s *angelOneLiveMarketStream) Receive(ctx context.Context) (LiveMarketEvent, error) {
	for {
		if err := ctx.Err(); err != nil {
			_ = s.Close()
			return LiveMarketEvent{}, err
		}
		messageType, payload, err := s.readMessage(ctx)
		if err != nil {
			return LiveMarketEvent{}, err
		}
		if messageType == websocket.TextMessage {
			text := strings.TrimSpace(string(payload))
			switch strings.ToLower(text) {
			case "pong":
				continue
			case "ping":
				if err := s.writeText("pong"); err != nil {
					return LiveMarketEvent{}, fmt.Errorf("reply to Angel One heartbeat: %w", err)
				}
				continue
			default:
				return LiveMarketEvent{}, angelOneWebSocketTextError(payload)
			}
		}

		packet, err := DecodeAngelOneMarketData(payload)
		if err != nil {
			// A malformed market packet should not terminate a healthy socket.
			// The next binary message can still contain a valid instrument tick.
			continue
		}
		event, err := packet.ToLiveMarketEvent()
		if err != nil {
			continue
		}
		subscription, exists := s.tokenMap[packet.Token]
		if !exists {
			// Unknown tokens are ignored so one unexpected provider packet cannot
			// poison the multiplexed stream for known MoneyPlant instruments.
			continue
		}
		event.CanonicalSymbol = subscription.CanonicalSymbol
		event.ProviderSymbol = subscription.ProviderSymbol
		return event, nil
	}
}

func (s *angelOneLiveMarketStream) readMessage(ctx context.Context) (int, []byte, error) {
	type readResult struct {
		messageType int
		payload     []byte
		err         error
	}
	resultChannel := make(chan readResult, 1)
	go func() {
		messageType, payload, err := s.connection.ReadMessage()
		resultChannel <- readResult{messageType: messageType, payload: payload, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = s.Close()
		return 0, nil, ctx.Err()
	case result := <-resultChannel:
		if result.err != nil {
			return 0, nil, fmt.Errorf("read Angel One live message: %w", result.err)
		}
		return result.messageType, result.payload, nil
	}
}

func (s *angelOneLiveMarketStream) writeText(text string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.connection.WriteMessage(websocket.TextMessage, []byte(text))
}

func (s *angelOneLiveMarketStream) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		if s.heartbeatDone != nil {
			close(s.heartbeatDone)
		}
		s.writeMu.Lock()
		closeErr = s.connection.Close()
		s.writeMu.Unlock()
	})
	return closeErr
}

type angelOneWebSocketError struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

func angelOneWebSocketTextError(payload []byte) error {
	var providerError angelOneWebSocketError
	if err := json.Unmarshal(payload, &providerError); err == nil && (providerError.ErrorCode != "" || providerError.ErrorMessage != "") {
		return fmt.Errorf("Angel One WebSocket provider error (%s): %s", providerError.ErrorCode, safeProviderMessage(providerError.ErrorMessage))
	}
	return fmt.Errorf("unexpected Angel One WebSocket text message")
}
