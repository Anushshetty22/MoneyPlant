package httpapi_test

import (
	// context is used by the store callback and keeps the test explicit about
	// the request-independent write operation.
	"context"
	// encoding/json decodes the public response envelope without depending on
	// the handler's unexported response type.
	"encoding/json"
	// net/http provides request and status-code constants for the HTTP checks.
	"net/http"
	// net/http/httptest runs the real API handler without opening a machine port.
	"net/http/httptest"
	// testing provides test assertions and failure reporting.
	"testing"
	// time creates deterministic event timestamps for the response assertions.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/httpapi"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestLiveSnapshotEndpointReturnsLatestEvents verifies the Phase 2.6 HTTP
// boundary with the same in-memory store used by the live monitor.
func TestLiveSnapshotEndpointReturnsLatestEvents(t *testing.T) {
	store := ingestion.NewLiveMarketSnapshotStore()
	observedAt := time.Date(2026, time.September, 8, 12, 30, 0, 0, time.UTC)

	if err := store.Handle(context.Background(), ingestion.LiveMarketEvent{
		ProviderSymbol:   "BTCUSDT",
		EventType:        "trade",
		ObservedAt:       observedAt,
		Price:            testNumeric(t, "64323.61000000"),
		Quantity:         testNumeric(t, "0.01250000"),
		SourceReceivedAt: observedAt.Add(250 * time.Millisecond),
	}); err != nil {
		t.Fatalf("store live event: %v", err)
	}

	server := httpapi.NewServer("127.0.0.1", 0, nil, nil, nil, nil, nil, store)
	testServer := httptest.NewServer(server.Handler)
	defer testServer.Close()

	t.Run("lists snapshots", func(t *testing.T) {
		response := getSnapshotResponse(t, testServer.URL+"/api/v1/live/snapshots")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", response.StatusCode, http.StatusOK, response.Body)
		}
		if len(response.Data) != 1 {
			t.Fatalf("snapshot count = %d, want 1", len(response.Data))
		}
		if response.Data[0].ProviderSymbol != "BTCUSDT" {
			t.Fatalf("symbol = %q, want BTCUSDT", response.Data[0].ProviderSymbol)
		}
		if response.Data[0].Price != "64323.61000000" {
			t.Fatalf("price = %q, want exact decimal string", response.Data[0].Price)
		}
		if response.Data[0].Quantity != "0.01250000" {
			t.Fatalf("quantity = %q, want exact decimal string", response.Data[0].Quantity)
		}
		if response.Data[0].ObservedAt != "2026-09-08T12:30:00Z" {
			t.Fatalf("observed_at = %q, want UTC RFC3339 value", response.Data[0].ObservedAt)
		}
	})

	t.Run("filters by symbol", func(t *testing.T) {
		response := getSnapshotResponse(t, testServer.URL+"/api/v1/live/snapshots?symbol=btcusdt")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", response.StatusCode, http.StatusOK, response.Body)
		}
		if len(response.Data) != 1 || response.Data[0].ProviderSymbol != "BTCUSDT" {
			t.Fatalf("filtered data = %#v, want one BTCUSDT snapshot", response.Data)
		}
	})

	t.Run("unknown symbol returns empty array", func(t *testing.T) {
		response := getSnapshotResponse(t, testServer.URL+"/api/v1/live/snapshots?symbol=ETHUSDT")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", response.StatusCode, http.StatusOK, response.Body)
		}
		if response.Data == nil || len(response.Data) != 0 {
			t.Fatalf("data = %#v, want an empty JSON array", response.Data)
		}
	})
}

// TestLiveSnapshotEndpointReturnsEmptyArrayWhenDisabled documents the normal
// API behavior when LIVE_MONITOR_SYMBOL is empty and no stream has been started.
func TestLiveSnapshotEndpointReturnsEmptyArrayWhenDisabled(t *testing.T) {
	store := ingestion.NewLiveMarketSnapshotStore()
	server := httpapi.NewServer("127.0.0.1", 0, nil, nil, nil, nil, nil, store)
	testServer := httptest.NewServer(server.Handler)
	defer testServer.Close()

	response := getSnapshotResponse(t, testServer.URL+"/api/v1/live/snapshots")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.StatusCode, http.StatusOK, response.Body)
	}
	if response.Data == nil || len(response.Data) != 0 {
		t.Fatalf("data = %#v, want an empty JSON array", response.Data)
	}
}

type snapshotResponse struct {
	ProviderSymbol   string `json:"provider_symbol"`
	EventType        string `json:"event_type"`
	ObservedAt       string `json:"observed_at"`
	Price            string `json:"price"`
	Quantity         string `json:"quantity"`
	SourceReceivedAt string `json:"source_received_at"`
}

type snapshotEnvelope struct {
	Data       []snapshotResponse `json:"data"`
	Body       string
	StatusCode int
}

func getSnapshotResponse(t *testing.T, url string) snapshotEnvelope {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()

	var envelope struct {
		Data []snapshotResponse `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return snapshotEnvelope{
		Data:       envelope.Data,
		StatusCode: response.StatusCode,
	}
}

func testNumeric(t *testing.T, value string) pgtype.Numeric {
	t.Helper()
	var numericValue pgtype.Numeric
	if err := numericValue.Scan(value); err != nil {
		t.Fatalf("parse test decimal %q: %v", value, err)
	}
	return numericValue
}
