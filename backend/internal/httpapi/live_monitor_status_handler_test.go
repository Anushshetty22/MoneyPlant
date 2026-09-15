package httpapi_test

import (
	// encoding/json decodes the public status envelope.
	"encoding/json"
	// errors provides a deterministic reconnect error for the status contract.
	"errors"
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
	statusStore.RecordReconnect(errors.New("temporary Binance disconnect"))

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
			Enabled              bool    `json:"enabled"`
			Provider             string  `json:"provider"`
			ProviderSymbol       string  `json:"provider_symbol"`
			State                string  `json:"state"`
			Accepted             int64   `json:"accepted"`
			Reconnects           int64   `json:"reconnects"`
			Persisted            int64   `json:"persisted"`
			Restored             int64   `json:"restored"`
			LastError            *string `json:"last_error"`
			LastReconnectError   *string `json:"last_reconnect_error"`
			LastPersistedAt      *string `json:"last_persisted_at"`
			LastPersistenceError *string `json:"last_persistence_error"`
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
	if envelope.Data.Reconnects != 1 {
		t.Fatalf("reconnects = %d, want 1", envelope.Data.Reconnects)
	}
	if envelope.Data.LastReconnectError == nil || *envelope.Data.LastReconnectError != "temporary Binance disconnect" {
		t.Fatalf("last_reconnect_error = %#v, want temporary Binance disconnect", envelope.Data.LastReconnectError)
	}
	if envelope.Data.Persisted != 0 || envelope.Data.Restored != 0 || envelope.Data.LastPersistedAt != nil || envelope.Data.LastPersistenceError != nil {
		t.Fatalf("unexpected persistence status: %#v", envelope.Data)
	}
	if envelope.Data.LastError != nil {
		t.Fatalf("last_error = %q, want null", *envelope.Data.LastError)
	}
}

func TestLiveMonitorStatusesEndpointReturnsIndependentProviderStates(t *testing.T) {
	registry := ingestion.NewLiveMonitorStatusRegistry()
	binanceStore := registry.Register(ingestion.InstrumentReference{
		CanonicalSymbol: "BTCUSDT", Provider: ingestion.ProviderBinance, ProviderSymbol: "BTCUSDT",
	})
	angelStore := registry.Register(ingestion.InstrumentReference{
		CanonicalSymbol: "NIFTY50", Provider: ingestion.ProviderAngelOne, ProviderSymbol: "Nifty 50",
	})
	binanceStore.MarkRunning()
	angelStore.MarkError(errors.New("Angel One credentials unavailable"))

	server := httpapi.NewServer("127.0.0.1", 0, nil, nil, nil, nil, nil, ingestion.NewLiveMarketSnapshotStore(), binanceStore, registry)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/live/statuses", nil)
	responseRecorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	var envelope struct {
		Data []struct {
			Provider       string `json:"provider"`
			Canonical      string `json:"canonical_symbol"`
			ProviderSymbol string `json:"provider_symbol"`
			State          string `json:"state"`
		} `json:"data"`
	}
	if err := json.NewDecoder(responseRecorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode statuses response: %v", err)
	}
	if len(envelope.Data) != 2 {
		t.Fatalf("status count = %d, want 2", len(envelope.Data))
	}
	if envelope.Data[0].Provider != "angel_one" || envelope.Data[0].Canonical != "NIFTY50" || envelope.Data[0].ProviderSymbol != "Nifty 50" || envelope.Data[0].State != ingestion.LiveMonitorStateError {
		t.Fatalf("first status = %#v", envelope.Data[0])
	}
	if envelope.Data[1].Provider != "binance" || envelope.Data[1].Canonical != "BTCUSDT" || envelope.Data[1].State != ingestion.LiveMonitorStateRunning {
		t.Fatalf("second status = %#v", envelope.Data[1])
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/live/status?symbol=NIFTY50&provider=angel_one", nil)
	responseRecorder = httptest.NewRecorder()
	server.Handler.ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("filtered status = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	var filtered struct {
		Data struct {
			Provider       string `json:"provider"`
			Canonical      string `json:"canonical_symbol"`
			ProviderSymbol string `json:"provider_symbol"`
		} `json:"data"`
	}
	if err := json.NewDecoder(responseRecorder.Body).Decode(&filtered); err != nil {
		t.Fatalf("decode filtered status: %v", err)
	}
	if filtered.Data.Provider != "angel_one" || filtered.Data.Canonical != "NIFTY50" || filtered.Data.ProviderSymbol != "Nifty 50" {
		t.Fatalf("filtered status = %#v", filtered.Data)
	}
}
