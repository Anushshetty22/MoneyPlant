package ingestion

import (
	// context lets cancellation interrupt connection attempts and backoff waits.
	"context"
	// errors identifies context cancellation separately from transport failures.
	"errors"
	// fmt adds retry information to the final stream error.
	"fmt"
	// strings validates the requested symbol before constructing the wrapper.
	"strings"
	// sync protects the current stream when shutdown happens while Receive is
	// waiting for a provider message.
	"sync"
	// time implements the default context-aware backoff sleep.
	"time"
)

// LiveReconnectPolicy controls how a live stream retries after a connection
// failure. MaxRetries counts attempts after the initial connection/read attempt.
// Therefore MaxRetries=0 means fail immediately, while MaxRetries=2 allows up
// to three total attempts.
type LiveReconnectPolicy struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Sleep          func(context.Context, time.Duration) error
}

// DefaultLiveReconnectPolicy provides conservative local defaults. The values
// are deliberately finite and bounded so an unavailable provider does not make
// a command or test hang forever.
func DefaultLiveReconnectPolicy() LiveReconnectPolicy {
	return LiveReconnectPolicy{
		MaxRetries:     3,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		Sleep:          sleepWithContext,
	}
}

// ReconnectingLiveMarketStream wraps a provider stream and reopens it after a
// transport or decoding failure.
//
// The wrapper owns every stream it opens. Callers still use the same
// LiveMarketStream interface, so adding reconnect behavior does not change the
// LiveMarketMonitor or any future event handler.
type ReconnectingLiveMarketStream struct {
	provider LiveMarketDataProvider
	request  LiveMarketStreamRequest
	policy   LiveReconnectPolicy

	mu      sync.Mutex
	current LiveMarketStream
	closed  bool
}

// NewReconnectingLiveMarketStream creates a retrying stream wrapper.
func NewReconnectingLiveMarketStream(
	provider LiveMarketDataProvider,
	request LiveMarketStreamRequest,
	policy LiveReconnectPolicy,
) (*ReconnectingLiveMarketStream, error) {
	if provider == nil {
		return nil, fmt.Errorf("live market provider cannot be nil")
	}
	if strings.TrimSpace(request.ProviderSymbol) == "" {
		return nil, fmt.Errorf("live market provider symbol cannot be empty")
	}
	if err := validateLiveReconnectPolicy(policy); err != nil {
		return nil, err
	}

	return &ReconnectingLiveMarketStream{
		provider: provider,
		request:  request,
		policy:   policy,
	}, nil
}

// Receive returns the next event, reconnecting when the current stream fails.
//
// A successful event resets the retry sequence because the connection is
// healthy again. A context cancellation is returned directly and never retried.
func (s *ReconnectingLiveMarketStream) Receive(ctx context.Context) (LiveMarketEvent, error) {
	var lastErr error

	for retry := 0; ; retry++ {
		if err := ctx.Err(); err != nil {
			return LiveMarketEvent{}, err
		}

		stream, err := s.ensureConnected(ctx)
		if err == nil {
			var event LiveMarketEvent
			event, err = stream.Receive(ctx)
			if err == nil {
				return event, nil
			}
			// A failed Receive invalidates this connection. Close it before
			// opening a replacement so resources do not accumulate.
			s.dropStream(stream)
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return LiveMarketEvent{}, err
		}
		lastErr = err
		if retry >= s.policy.MaxRetries {
			return LiveMarketEvent{}, fmt.Errorf("live market stream failed after %d retries: %w", retry, lastErr)
		}

		backoff := reconnectBackoff(s.policy, retry)
		if err := s.policy.Sleep(ctx, backoff); err != nil {
			return LiveMarketEvent{}, fmt.Errorf("wait before live stream retry: %w", err)
		}
	}
}

// Close stops future reconnects and closes the currently open provider stream.
func (s *ReconnectingLiveMarketStream) Close() error {
	s.mu.Lock()
	s.closed = true
	current := s.current
	s.current = nil
	s.mu.Unlock()

	if current == nil {
		return nil
	}
	return current.Close()
}

// ensureConnected opens one provider stream when no current stream exists.
func (s *ReconnectingLiveMarketStream) ensureConnected(ctx context.Context) (LiveMarketStream, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("reconnecting live market stream is closed")
	}
	if s.current != nil {
		current := s.current
		s.mu.Unlock()
		return current, nil
	}
	s.mu.Unlock()

	stream, err := s.provider.OpenTradeStream(ctx, s.request)
	if err != nil {
		return nil, fmt.Errorf("open live market stream: %w", err)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = stream.Close()
		return nil, fmt.Errorf("reconnecting live market stream is closed")
	}
	s.current = stream
	s.mu.Unlock()
	return stream, nil
}

// dropStream removes and closes the failed stream. The wrapper is used
// sequentially by the monitor, so clearing the current pointer is sufficient;
// the mutex still makes concurrent Close safe during process shutdown.
func (s *ReconnectingLiveMarketStream) dropStream(stream LiveMarketStream) {
	s.mu.Lock()
	if s.current == stream {
		s.current = nil
	}
	s.mu.Unlock()
	_ = stream.Close()
}

func validateLiveReconnectPolicy(policy LiveReconnectPolicy) error {
	if policy.MaxRetries < 0 {
		return fmt.Errorf("live reconnect MaxRetries cannot be negative")
	}
	if policy.InitialBackoff <= 0 {
		return fmt.Errorf("live reconnect InitialBackoff must be positive")
	}
	if policy.MaxBackoff < policy.InitialBackoff {
		return fmt.Errorf("live reconnect MaxBackoff cannot be smaller than InitialBackoff")
	}
	if policy.Sleep == nil {
		return fmt.Errorf("live reconnect Sleep function cannot be nil")
	}
	return nil
}

// reconnectBackoff doubles after each failed attempt and caps at MaxBackoff.
func reconnectBackoff(policy LiveReconnectPolicy, retry int) time.Duration {
	backoff := policy.InitialBackoff
	for index := 0; index < retry; index++ {
		if backoff >= policy.MaxBackoff/2 {
			return policy.MaxBackoff
		}
		backoff *= 2
	}
	if backoff > policy.MaxBackoff {
		return policy.MaxBackoff
	}
	return backoff
}

// sleepWithContext lets cancellation interrupt a backoff instead of waiting
// for the complete delay.
func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
