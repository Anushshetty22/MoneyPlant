package httpapi_test

import (
	// context gives database startup and HTTP requests a finite deadline.
	"context"
	// encoding/json decodes the API envelope without duplicating production
	// response types inside the test package.
	"encoding/json"
	// io reads the complete response body so the test can decode it in assertions.
	"io"
	// net/http sends requests to the in-memory HTTP test server.
	"net/http"
	// net/http/httptest runs the real API handler without binding a machine port.
	"net/http/httptest"
	// os controls the opt-in integration-test behavior.
	"os"
	// testing provides test execution, failure reporting, and skip behavior.
	"testing"
	// time supplies the integration-test deadline.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/httpapi"
)

// TestReadOnlyAPIIntegration verifies the complete read-only HTTP surface using
// the real repositories and the real PostgreSQL database.
//
// Phase 6 completion update: this test was added so API verification is
// repeatable. It does not insert or modify data; it reads the seeded MoneyPlant
// records and checks the response status and data envelope for every endpoint.
func TestReadOnlyAPIIntegration(t *testing.T) {
	// Keep ordinary unit-test runs independent of Docker and local database state.
	// The explicit flag makes the database dependency visible when this test is
	// intentionally requested by the developer.
	if os.Getenv("MONEYPLANT_RUN_INTEGRATION") != "1" {
		t.Skip("set MONEYPLANT_RUN_INTEGRATION=1 to run API integration tests")
	}

	// Use the same configuration and pool constructor as cmd/api. This ensures
	// the test exercises real connection setup rather than a test-only database
	// configuration path.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load test configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("create test database pool: %v", err)
	}
	defer pool.Close()

	// Build every repository exactly as main.go does. The HTTP server receives
	// these dependencies through its constructor, so the test covers dependency
	// wiring as well as handler behavior.
	server := httpapi.NewServer(
		"127.0.0.1",
		0,
		database.NewInstrumentRepository(pool),
		database.NewMarketCandleRepository(pool),
		database.NewMacroDatasetRepository(pool),
		database.NewMacroObservationRepository(pool),
		database.NewIngestionRunRepository(pool),
	)

	// httptest.NewServer uses an ephemeral local port and serves the exact Handler
	// registered by the production server. No separate application process is
	// needed, which keeps this test fast while still exercising HTTP behavior.
	testServer := httptest.NewServer(server.Handler)
	defer testServer.Close()

	t.Run("health", func(t *testing.T) {
		response := getAPIResponse(t, testServer.Client(), testServer.URL+"/health")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("health status = %d, want %d", response.StatusCode, http.StatusOK)
		}
	})

	t.Run("instruments", func(t *testing.T) {
		response := getAPIResponse(t, testServer.Client(), testServer.URL+"/api/v1/instruments")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("instruments status = %d, want %d", response.StatusCode, http.StatusOK)
		}
		assertDataCountAtLeast(t, response.Body, 2)
	})

	t.Run("binance candles", func(t *testing.T) {
		url := testServer.URL + "/api/v1/candles?symbol=BTCUSDT&provider=binance&interval=1d&from=2026-08-01T00:00:00Z&to=2026-08-07T00:00:00Z"
		response := getAPIResponse(t, testServer.Client(), url)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("candles status = %d, want %d", response.StatusCode, http.StatusOK)
		}
		assertDataCountAtLeast(t, response.Body, 1)
	})

	t.Run("macro datasets", func(t *testing.T) {
		response := getAPIResponse(t, testServer.Client(), testServer.URL+"/api/v1/macro/datasets")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("macro datasets status = %d, want %d", response.StatusCode, http.StatusOK)
		}
		assertDataCountAtLeast(t, response.Body, 2)
	})

	t.Run("macro observations", func(t *testing.T) {
		url := testServer.URL + "/api/v1/macro/observations?dataset=rbi_cpi_combined_yoy&from=2026-01-01&to=2026-03-01"
		response := getAPIResponse(t, testServer.Client(), url)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("macro observations status = %d, want %d", response.StatusCode, http.StatusOK)
		}
		assertDataCountAtLeast(t, response.Body, 1)
	})

	t.Run("ingestion history", func(t *testing.T) {
		response := getAPIResponse(t, testServer.Client(), testServer.URL+"/api/v1/ingestion-runs?limit=5")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("ingestion history status = %d, want %d", response.StatusCode, http.StatusOK)
		}
		assertDataCountAtLeast(t, response.Body, 1)
	})

	t.Run("invalid candle request", func(t *testing.T) {
		response := getAPIResponse(t, testServer.Client(), testServer.URL+"/api/v1/candles?symbol=BTCUSDT")
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid candle status = %d, want %d", response.StatusCode, http.StatusBadRequest)
		}
	})
}

// apiResponse contains only the information needed by the test helpers. The
// body is retained until a subtest decodes it so failures can identify malformed
// JSON or an unexpected response envelope.
type apiResponse struct {
	StatusCode int
	Body       []byte
}

// getAPIResponse sends a GET request and reads its complete body. Closing the
// response here prevents the HTTP transport from leaking a connection between
// subtests.
func getAPIResponse(t *testing.T, client *http.Client, url string) apiResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create GET request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send GET request: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read API response: %v", err)
	}
	return apiResponse{StatusCode: response.StatusCode, Body: body}
}

// assertDataCountAtLeast checks the common {"data": [...]} response envelope
// and verifies that seeded data reached the HTTP boundary.
func assertDataCountAtLeast(t *testing.T, body []byte, minimum int) {
	t.Helper()
	var envelope struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode data envelope: %v; body=%s", err, body)
	}
	if len(envelope.Data) < minimum {
		t.Fatalf("data count = %d, want at least %d; body=%s", len(envelope.Data), minimum, body)
	}
}
