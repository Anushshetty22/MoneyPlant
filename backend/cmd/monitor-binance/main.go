// Command monitor-binance connects the Phase 2 live-data components into one
// user-runnable monitor. It intentionally does not write to PostgreSQL yet;
// the command demonstrates the real-time flow and keeps the latest events in
// the in-memory snapshot store.
package main

import (
	// context carries cancellation from Ctrl+C through the WebSocket monitor.
	"context"
	// encoding/json formats exact pgtype.Numeric values without converting them
	// through float64 for log output.
	"encoding/json"
	// errors lets the command treat expected context cancellation as a clean exit.
	"errors"
	// flag parses simple command-line options for the learning workflow.
	"flag"
	// fmt formats the final monitoring summary and returned errors.
	"fmt"
	// log prints each received live event and lifecycle message.
	"log"
	// os and os/signal allow the command to stop when the user presses Ctrl+C.
	"os"
	"os/signal"
	// strings validates the symbol before opening a network connection.
	"strings"
	// syscall includes SIGTERM for normal process-manager or container shutdown.
	"syscall"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

func main() {
	symbol := flag.String("symbol", "BTCUSDT", "Binance symbol to monitor")
	eventLimit := flag.Int("events", 0, "stop after this many events; 0 means keep monitoring")
	maxRetries := flag.Int("max-retries", 3, "number of reconnect retries after a stream failure")
	wsURL := flag.String("ws-url", ingestion.BinanceSpotWebSocketBaseURL, "Binance WebSocket base URL")
	flag.Parse()

	if strings.TrimSpace(*symbol) == "" {
		log.Fatal("--symbol cannot be empty")
	}
	if *eventLimit < 0 {
		log.Fatal("--events cannot be negative")
	}
	if *maxRetries < 0 {
		log.Fatal("--max-retries cannot be negative")
	}

	// NotifyContext cancels the monitor when the user presses Ctrl+C or when a
	// process manager sends SIGTERM. The monitor then closes its WebSocket.
	monitorContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runMonitor(monitorContext, *symbol, *eventLimit, *maxRetries, *wsURL); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.Printf("monitor stopped: %v", err)
			return
		}
		log.Fatalf("monitoring failed: %v", err)
	}
}

// runMonitor composes the Phase 2 components:
//
//  1. BinanceLiveMarketDataProvider opens and decodes Binance messages.
//  2. ReconnectingLiveMarketStream recovers from connection failures.
//  3. LiveMarketMonitor validates normalized events.
//  4. LiveMarketSnapshotStore keeps the latest event for each symbol.
//
// Keeping this composition in a function makes the command's lifecycle easy to
// read and gives future tests a single orchestration boundary.
func runMonitor(ctx context.Context, symbol string, eventLimit, maxRetries int, wsURL string) error {
	provider, err := ingestion.NewBinanceLiveMarketDataProvider(nil, wsURL)
	if err != nil {
		return fmt.Errorf("create Binance live provider: %w", err)
	}

	policy := ingestion.DefaultLiveReconnectPolicy()
	policy.MaxRetries = maxRetries
	stream, err := ingestion.NewReconnectingLiveMarketStream(
		provider,
		ingestion.LiveMarketStreamRequest{ProviderSymbol: symbol},
		policy,
	)
	if err != nil {
		return fmt.Errorf("create reconnecting stream: %w", err)
	}
	defer stream.Close()

	store := ingestion.NewLiveMarketSnapshotStore()
	monitor, err := ingestion.NewLiveMarketMonitor(stream)
	if err != nil {
		return fmt.Errorf("create live market monitor: %w", err)
	}

	// Use a child context for the bounded --events mode. Once the requested
	// number of events has been handled, cancellation releases the blocked
	// Receive call and lets the monitor close the stream normally.
	monitorContext, stop := context.WithCancel(ctx)
	defer stop()

	processedEvents := 0
	result, err := monitor.Run(monitorContext, func(handlerContext context.Context, event ingestion.LiveMarketEvent) error {
		if err := store.Handle(handlerContext, event); err != nil {
			return err
		}
		processedEvents++
		log.Printf(
			"live trade symbol=%s price=%s quantity=%s observed_at=%s",
			event.ProviderSymbol,
			numericText(event.Price),
			numericText(event.Quantity),
			event.ObservedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		)

		if eventLimit > 0 && processedEvents >= eventLimit {
			stop()
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	log.Printf("monitor summary: received=%d accepted=%d rejected=%d snapshots=%d", result.Received, result.Accepted, result.Rejected, store.Count())
	return err
}

// numericText serializes pgtype.Numeric directly so log output preserves the
// source decimal scale, for example 64323.61000000 instead of a rounded float.
func numericText(value pgtype.Numeric) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<invalid numeric: %v>", err)
	}
	return string(encoded)
}
