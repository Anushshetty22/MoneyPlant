package ingestion_test

import (
	// context supplies cancellation to the reconnecting wrapper.
	"context"
	// errors creates deterministic fake transport failures.
	"errors"
	// io represents a cleanly closed fake connection.
	"io"
	// testing provides assertions.
	"testing"
	// time supplies exact backoff values and event timestamps.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// TestReconnectingLiveMarketStreamRetriesAfterDisconnect verifies that one
// failed stream is closed and replaced before the next event is returned.
func TestReconnectingLiveMarketStreamRetriesAfterDisconnect(t *testing.T) {
	firstStream := &scriptedLiveStream{receiveErrors: []error{errors.New("connection dropped")}}
	secondStream := &scriptedLiveStream{events: []ingestion.LiveMarketEvent{reconnectEvent(t)}}
	provider := &scriptedLiveProvider{streams: []ingestion.LiveMarketStream{firstStream, secondStream}}
	delays := make([]time.Duration, 0)
	policy := testReconnectPolicy(func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})

	stream, err := ingestion.NewReconnectingLiveMarketStream(provider, ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTCUSDT"}, policy)
	if err != nil {
		t.Fatalf("create reconnecting stream: %v", err)
	}
	defer stream.Close()

	event, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive after reconnect: %v", err)
	}
	if event.ProviderSymbol != "BTCUSDT" {
		t.Errorf("symbol = %q, want BTCUSDT", event.ProviderSymbol)
	}
	if provider.openCount != 2 {
		t.Errorf("open count = %d, want 2", provider.openCount)
	}
	if !firstStream.closed {
		t.Error("failed stream was not closed before reconnect")
	}
	if len(delays) != 1 || delays[0] != 10*time.Millisecond {
		t.Errorf("backoff delays = %v, want [10ms]", delays)
	}
}

// TestReconnectingLiveMarketStreamStopsAtRetryLimit verifies that an
// unavailable provider produces a bounded error rather than retrying forever.
func TestReconnectingLiveMarketStreamStopsAtRetryLimit(t *testing.T) {
	provider := &scriptedLiveProvider{
		openErrors: []error{
			errors.New("provider unavailable"),
			errors.New("provider unavailable"),
			errors.New("provider unavailable"),
		},
	}
	policy := testReconnectPolicy(func(_ context.Context, _ time.Duration) error { return nil })
	policy.MaxRetries = 2

	stream, err := ingestion.NewReconnectingLiveMarketStream(provider, ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTCUSDT"}, policy)
	if err != nil {
		t.Fatalf("create reconnecting stream: %v", err)
	}
	defer stream.Close()

	if _, err := stream.Receive(context.Background()); err == nil {
		t.Fatal("expected bounded retry error, got nil")
	}
	if provider.openCount != 3 {
		t.Errorf("open count = %d, want 3 total attempts", provider.openCount)
	}
}

// TestReconnectingLiveMarketStreamCancellationStopsBackoff verifies that a
// caller can stop the wrapper while it is waiting between retry attempts.
func TestReconnectingLiveMarketStreamCancellationStopsBackoff(t *testing.T) {
	provider := &scriptedLiveProvider{openErrors: []error{errors.New("provider unavailable")}}
	policy := ingestion.DefaultLiveReconnectPolicy()
	policy.Sleep = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}
	stream, err := ingestion.NewReconnectingLiveMarketStream(provider, ingestion.LiveMarketStreamRequest{ProviderSymbol: "BTCUSDT"}, policy)
	if err != nil {
		t.Fatalf("create reconnecting stream: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resultChannel := make(chan error, 1)
	go func() {
		_, receiveErr := stream.Receive(ctx)
		resultChannel <- receiveErr
	}()
	cancel()

	if err := <-resultChannel; err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
}

func testReconnectPolicy(sleep func(context.Context, time.Duration) error) ingestion.LiveReconnectPolicy {
	return ingestion.LiveReconnectPolicy{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     40 * time.Millisecond,
		Sleep:          sleep,
	}
}

type scriptedLiveProvider struct {
	streams    []ingestion.LiveMarketStream
	openErrors []error
	openCount  int
}

func (p *scriptedLiveProvider) ProviderName() string { return "scripted" }

func (p *scriptedLiveProvider) OpenTradeStream(_ context.Context, _ ingestion.LiveMarketStreamRequest) (ingestion.LiveMarketStream, error) {
	index := p.openCount
	p.openCount++
	if index < len(p.openErrors) && p.openErrors[index] != nil {
		return nil, p.openErrors[index]
	}
	if index >= len(p.streams) {
		return nil, errors.New("no scripted stream available")
	}
	return p.streams[index], nil
}

type scriptedLiveStream struct {
	events        []ingestion.LiveMarketEvent
	receiveErrors []error
	index         int
	closed        bool
}

func (s *scriptedLiveStream) Receive(_ context.Context) (ingestion.LiveMarketEvent, error) {
	if s.index < len(s.receiveErrors) && s.receiveErrors[s.index] != nil {
		err := s.receiveErrors[s.index]
		s.index++
		return ingestion.LiveMarketEvent{}, err
	}
	eventIndex := s.index - len(s.receiveErrors)
	if eventIndex >= 0 && eventIndex < len(s.events) {
		s.index++
		return s.events[eventIndex], nil
	}
	return ingestion.LiveMarketEvent{}, io.EOF
}

func (s *scriptedLiveStream) Close() error {
	s.closed = true
	return nil
}

func reconnectEvent(t *testing.T) ingestion.LiveMarketEvent {
	t.Helper()
	return ingestion.LiveMarketEvent{
		ProviderSymbol:   "BTCUSDT",
		EventType:        "trade",
		ObservedAt:       time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC),
		Price:            liveNumeric(t, "100.00"),
		Quantity:         liveNumeric(t, "1.00"),
		SourceReceivedAt: time.Date(2026, time.August, 12, 10, 0, 1, 0, time.UTC),
	}
}
