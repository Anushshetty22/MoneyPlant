package httpapi_test

import (
	// encoding/json decodes the public status envelope.
	"encoding/json"
	// net/http provides request and status-code constants.
	"net/http"
	// net/http/httptest serves the real API handler without a machine port.
	"net/http/httptest"
	// testing provides assertions.
	"testing"
	// time creates deterministic event timestamps.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/httpapi"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// TestLiveMonitorStatusEndpointReturnsOperationalState verifies that lifecycle
// state and counters are visible independently from the latest snapshot value.
func TestLiveMonitorStatusEndpointReturnsOperationalState(t *testing.T) {
	statusStore := ingestion.NewLiveMonitorStatusStore()
	statusStore.Configure("binance", "BTCUSDT")
	statusStore.MarkRunning()

	eventTime := time.Date(2026, time.September, 8, 12, 30, 0, 0, time.UTC)
	statusStore.RecordAccepted(ingestion.LiveMarketEvent{
		ProviderSymbol:   "BTCUSDT",
		EventType:        "trade",
		ObservedAt:       eventTime,
		Price:            testNumeric(t, "64323.61"),
		Quantity:         testNumeric(t, "0.01"),
		SourceReceivedAt: eventTime.Add(time.Millisecond),
	})

	server := httpapi.NewServer("127.0.0.1", 0, nil, nil, nil, nil, nil, ingestion.NewLiveMarketSnapshotStore(), statusStore)
	testServer := httptest.NewServer(server.Handler)
	defer testServer.Close()

	response, err := http.Get(testServer.URL + "/api/v1/live/status")
	if err != nil {
		t.Fatalf("GET live status: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	var envelope struct {
		Data struct {
			Enabled        bool    `json:"enabled"`
			Provider       string  `json:"provider"`
			ProviderSymbol string  `json:"provider_symbol"`
			State          string  `json:"state"`
			Accepted       int64   `json:"accepted"`
			LastError      *string `json:"last_error"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode live status: %v", err)
	}

	if !envelope.Data.Enabled || envelope.Data.Provider != "binance" || envelope.Data.ProviderSymbol != "BTCUSDT" {
		t.Fatalf("unexpected monitor identity: %#v", envelope.Data)
	}
	if envelope.Data.State != ingestion.LiveMonitorStateRunning {
		t.Fatalf("state = %q, want %q", envelope.Data.State, ingestion.LiveMonitorStateRunning)
	}
	if envelope.Data.Accepted != 1 {
		t.Fatalf("accepted = %d, want 1", envelope.Data.Accepted)
	}
	if envelope.Data.LastError != nil {
		t.Fatalf("last_error = %q, want null", *envelope.Data.LastError)
	}
}
