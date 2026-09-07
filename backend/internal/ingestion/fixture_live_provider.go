package ingestion

import (
	// context allows a test or local caller to cancel Receive consistently with
	// a real network-backed stream.
	"context"
	// io supplies the normal end-of-stream result after all fixture events.
	"io"
	// fmt reports invalid subscription requests clearly.
	"fmt"
	// strings trims provider symbols before comparing them.
	"strings"
)

// FixtureLiveMarketDataProvider replays normalized events without network access.
//
// It is intentionally a provider implementation rather than a test-only helper:
// running a deterministic stream locally lets us learn the monitor lifecycle
// before credentials, WebSocket servers, and reconnect behavior are introduced.
type FixtureLiveMarketDataProvider struct {
	events []LiveMarketEvent
}

// NewFixtureLiveMarketDataProvider copies events so the caller cannot mutate the
// stream after it has been created. An empty fixture is valid and immediately
// produces io.EOF when consumed.
func NewFixtureLiveMarketDataProvider(events []LiveMarketEvent) *FixtureLiveMarketDataProvider {
	copiedEvents := make([]LiveMarketEvent, len(events))
	copy(copiedEvents, events)
	return &FixtureLiveMarketDataProvider{events: copiedEvents}
}

// ProviderName identifies the offline source in logs and future run metadata.
func (p *FixtureLiveMarketDataProvider) ProviderName() string {
	return "fixture"
}

// OpenTradeStream selects fixture events for the requested provider symbol.
func (p *FixtureLiveMarketDataProvider) OpenTradeStream(_ context.Context, request LiveMarketStreamRequest) (LiveMarketStream, error) {
	symbol := strings.TrimSpace(request.ProviderSymbol)
	if symbol == "" {
		return nil, fmt.Errorf("fixture provider symbol cannot be empty")
	}

	selectedEvents := make([]LiveMarketEvent, 0, len(p.events))
	for _, event := range p.events {
		if event.ProviderSymbol == symbol {
			selectedEvents = append(selectedEvents, event)
		}
	}
	return &fixtureLiveMarketStream{events: selectedEvents}, nil
}

// fixtureLiveMarketStream is a finite, in-memory implementation of
// LiveMarketStream. Its behavior mirrors the important part of a WebSocket
// consumer: receive one event, handle it, then request the next event.
type fixtureLiveMarketStream struct {
	events []LiveMarketEvent
	index  int
	closed bool
}

// Receive returns the next matching event or io.EOF after the fixture ends.
func (s *fixtureLiveMarketStream) Receive(ctx context.Context) (LiveMarketEvent, error) {
	select {
	case <-ctx.Done():
		return LiveMarketEvent{}, ctx.Err()
	default:
	}

	if s.closed || s.index >= len(s.events) {
		return LiveMarketEvent{}, io.EOF
	}

	event := s.events[s.index]
	s.index++
	return event, nil
}

// Close marks the finite stream closed. A real provider will use this method to
// close its WebSocket connection and release any transport resources.
func (s *fixtureLiveMarketStream) Close() error {
	s.closed = true
	return nil
}
