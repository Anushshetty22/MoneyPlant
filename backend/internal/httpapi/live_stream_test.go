package httpapi_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/httpapi"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

func TestLiveStreamSendsInitialAndPublishedUpdates(t *testing.T) {
	store := ingestion.NewLiveMarketSnapshotStore()
	eventTime := time.Date(2026, time.September, 14, 12, 30, 0, 0, time.UTC)
	event := ingestion.LiveMarketEvent{
		CanonicalSymbol:  "TCS",
		Provider:         ingestion.ProviderAngelOne,
		ProviderSymbol:   "TCS-EQ",
		EventType:        "trade",
		ObservedAt:       eventTime,
		Price:            testNumeric(t, "2200.80"),
		Quantity:         testNumeric(t, "14"),
		SourceReceivedAt: eventTime.Add(time.Millisecond),
	}
	if err := store.Handle(context.Background(), event); err != nil {
		t.Fatalf("store event: %v", err)
	}
	registry := ingestion.NewLiveMonitorStatusRegistry()
	statusStore := registry.Register(ingestion.InstrumentReference{
		CanonicalSymbol: "TCS", Provider: ingestion.ProviderAngelOne, ProviderSymbol: "TCS-EQ",
	})
	statusStore.MarkRunning()
	hub := httpapi.NewLiveStreamHub()
	server := httpapi.NewServerWithInstrumentSourcesAndLiveStream(
		"127.0.0.1", 0, nil, nil, nil, nil, nil, store,
		ingestion.NewLiveMonitorStatusStore(), nil, hub, registry,
	)
	testServer := httptest.NewServer(server.Handler)
	defer testServer.Close()

	response, err := testServer.Client().Get(testServer.URL + "/api/v1/live/stream?symbol=TCS&provider=angel_one")
	if err != nil {
		t.Fatalf("open live stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", contentType)
	}

	reader := bufio.NewReader(response.Body)
	initial := readSSEBlock(t, reader)
	if !strings.Contains(initial, "event: snapshot") || !strings.Contains(initial, "TCS-EQ") {
		t.Fatalf("initial stream block = %q, want TCS snapshot", initial)
	}

	event.Price = testNumeric(t, "2201.10")
	hub.PublishSnapshot(event)
	for attempt := 0; attempt < 6; attempt++ {
		published := readSSEBlock(t, reader)
		if strings.Contains(published, "event: snapshot") && strings.Contains(published, "2201.10") {
			return
		}
	}
	t.Fatal("updated snapshot was not received from SSE stream")
}

func readSSEBlock(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE block: %v", err)
		}
		lines = append(lines, line)
		if line == "\n" {
			return fmt.Sprint(lines)
		}
	}
}
