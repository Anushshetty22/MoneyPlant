package ingestion

import (
	// context lets a caller stop a long-running live stream during shutdown or
	// when a user changes the monitored symbol.
	"context"
	// errors lets the monitor recognize the normal end of an offline fixture.
	"errors"
	// fmt adds the stage that failed to returned streaming errors.
	"fmt"
	// io provides the conventional end-of-stream signal for the fixture and
	// future WebSocket implementations.
	"io"
	// math/big is used indirectly by finiteNumeric to compare exact decimals.
	// The import is kept here because the validation contract is about exact
	// numeric values rather than float64 approximations.
	"math/big"
	// strings normalizes provider symbols before checking that they are present.
	"strings"
	// time records the provider event time and the local receive time.
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// LiveMarketStreamRequest describes one live subscription. Phase 3.1 adds the
// canonical identity and optional provider token without changing the existing
// one-symbol execution boundary; multi-symbol orchestration is Phase 3.2.
type LiveMarketStreamRequest struct {
	CanonicalSymbol      string
	ProviderSymbol       string
	ProviderInstrumentID string
}

// LiveMarketEvent is the provider-neutral representation of one live trade.
//
// Live events are not stored in PostgreSQL yet. This model is the contract
// between a future WebSocket adapter and the monitoring/persistence layer. The
// price and quantity remain pgtype.Numeric values so a decimal such as
// "64323.61000000" is never rounded through float64.
type LiveMarketEvent struct {
	CanonicalSymbol  string
	Provider         ProviderID
	ProviderSymbol   string
	EventType        string
	ObservedAt       time.Time
	Price            pgtype.Numeric
	Quantity         pgtype.Numeric
	SourceReceivedAt time.Time
}

// LiveMarketDataProvider opens a normalized stream for one provider symbol.
//
// A real adapter will own WebSocket URL construction, authentication if needed,
// provider JSON decoding, reconnect policy, and provider error messages. The
// rest of the application only sees this small provider-independent interface.
type LiveMarketDataProvider interface {
	MarketDataProvider
	OpenTradeStream(context.Context, LiveMarketStreamRequest) (LiveMarketStream, error)
}

// LiveMarketStream returns one normalized event at a time.
//
// Receive should return io.EOF when a finite stream ends. A real WebSocket is
// normally open-ended, so it will keep waiting until the context is canceled
// or a transport error occurs. Close releases the underlying connection.
type LiveMarketStream interface {
	Receive(context.Context) (LiveMarketEvent, error)
	Close() error
}

// LiveMarketEventHandler receives each valid event accepted by the monitor.
//
// The callback is deliberately injected instead of hard-coding PostgreSQL.
// That keeps this first phase testable and allows later consumers such as a
// latest-price cache, candle builder, database writer, or alert engine.
type LiveMarketEventHandler func(context.Context, LiveMarketEvent) error

// LiveMarketRunResult summarizes one monitor lifetime.
//
// Received includes malformed events because they arrived from the provider.
// Accepted counts events passed to the callback, while Rejected counts events
// dropped by validation. LastEvent is useful for a future monitoring endpoint.
type LiveMarketRunResult struct {
	Received  int64
	Accepted  int64
	Rejected  int64
	LastEvent *LiveMarketEvent
}

// LiveMarketMonitor validates and consumes a live stream.
//
// The monitor does not know whether the stream came from a fixture, Binance,
// or another provider. Its job is to enforce the common event contract and
// provide predictable lifecycle behavior around cancellation and close.
type LiveMarketMonitor struct {
	stream LiveMarketStream
}

// NewLiveMarketMonitor creates a monitor that owns the supplied stream.
// Run will close the stream before returning, regardless of success or error.
func NewLiveMarketMonitor(stream LiveMarketStream) (*LiveMarketMonitor, error) {
	if stream == nil {
		return nil, fmt.Errorf("live market stream cannot be nil")
	}
	return &LiveMarketMonitor{stream: stream}, nil
}

// Run consumes events until the stream ends, the context is canceled, or the
// callback returns an error.
//
// Invalid events are counted and skipped instead of terminating the complete
// monitor. A single malformed provider message should not make a live monitor
// blind to all subsequent valid messages. Transport and callback errors remain
// fatal because the caller needs to decide whether to reconnect or stop.
func (m *LiveMarketMonitor) Run(ctx context.Context, handler LiveMarketEventHandler) (result LiveMarketRunResult, runErr error) {
	// The monitor owns the stream after construction. Closing it here keeps the
	// lifecycle safe for both finite fixtures and future network connections.
	defer func() {
		if closeErr := m.stream.Close(); runErr == nil && closeErr != nil {
			runErr = fmt.Errorf("close live market stream: %w", closeErr)
		}
	}()

	for {
		event, err := m.stream.Receive(ctx)
		if errors.Is(err, io.EOF) {
			// EOF is expected for the finite fixture and is a clean result.
			return result, nil
		}
		if err != nil {
			return result, fmt.Errorf("receive live market event: %w", err)
		}

		result.Received++
		if err := ValidateLiveMarketEvent(event); err != nil {
			result.Rejected++
			// Continue reading so one bad message does not stop the monitor.
			continue
		}

		if handler != nil {
			if err := handler(ctx, event); err != nil {
				return result, fmt.Errorf("handle live market event: %w", err)
			}
		}

		result.Accepted++
		// Copy the loop value before taking its address. This makes LastEvent
		// point to a stable value after the next Receive call overwrites event.
		lastEvent := event
		result.LastEvent = &lastEvent
	}
}

// ValidateLiveMarketEvent checks the common contract before a live event can
// reach a callback or a future persistence layer.
//
// A trade must have a symbol, the event type "trade", finite timestamps, a
// positive exact-decimal price, and a positive exact-decimal quantity. The
// receive timestamp cannot precede the provider event timestamp because the
// application can only receive an event after the provider emitted it.
func ValidateLiveMarketEvent(event LiveMarketEvent) error {
	if strings.TrimSpace(event.ProviderSymbol) == "" {
		return fmt.Errorf("provider symbol cannot be empty")
	}
	if event.EventType != "trade" {
		return fmt.Errorf("event type %q is not supported", event.EventType)
	}
	if event.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at must be present")
	}
	if event.SourceReceivedAt.IsZero() {
		return fmt.Errorf("source_received_at must be present")
	}
	if event.SourceReceivedAt.Before(event.ObservedAt) {
		return fmt.Errorf("source_received_at must not be before observed_at")
	}

	price, err := finiteNumeric(event.Price, "price")
	if err != nil {
		return err
	}
	quantity, err := finiteNumeric(event.Quantity, "quantity")
	if err != nil {
		return err
	}
	if price.Cmp(new(big.Rat)) <= 0 {
		return fmt.Errorf("price must be positive")
	}
	if quantity.Cmp(new(big.Rat)) <= 0 {
		return fmt.Errorf("quantity must be positive")
	}

	return nil
}

// numericFromString is a small test-and-fixture helper that parses a decimal
// without converting it through a binary floating-point type. Real adapters
// will use the same direct Scan approach when decoding provider JSON strings.
func numericFromString(value string) (pgtype.Numeric, error) {
	var numericValue pgtype.Numeric
	if err := numericValue.Scan(value); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("parse decimal %q: %w", value, err)
	}
	return numericValue, nil
}
