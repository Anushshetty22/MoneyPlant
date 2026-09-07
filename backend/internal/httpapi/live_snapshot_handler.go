package httpapi

import (
	// net/http provides request and response types plus HTTP status constants.
	"net/http"
	// strings normalizes an optional symbol filter.
	"strings"
	// time formats live event timestamps as UTC ISO-8601 strings.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// liveSnapshotResponse is the stable JSON representation exposed by the live
// monitoring endpoint. Decimal values remain strings for the same precision
// rule used by the historical candle API.
type liveSnapshotResponse struct {
	ProviderSymbol   string `json:"provider_symbol"`
	EventType        string `json:"event_type"`
	ObservedAt       string `json:"observed_at"`
	Price            string `json:"price"`
	Quantity         string `json:"quantity"`
	SourceReceivedAt string `json:"source_received_at"`
}

// listLiveSnapshotsHandler serves the latest in-memory event for every symbol,
// or one symbol when ?symbol=BTCUSDT is provided.
func listLiveSnapshotsHandler(
	responseWriter http.ResponseWriter,
	request *http.Request,
	store *ingestion.LiveMarketSnapshotStore,
) {
	if store == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "live snapshot store is not configured",
		})
		return
	}

	symbol := strings.ToUpper(strings.TrimSpace(request.URL.Query().Get("symbol")))
	var events []ingestion.LiveMarketEvent
	if symbol == "" {
		events = store.List()
	} else {
		event, exists := store.Get(symbol)
		if !exists {
			events = []ingestion.LiveMarketEvent{}
		} else {
			events = []ingestion.LiveMarketEvent{event}
		}
	}

	items := make([]liveSnapshotResponse, 0, len(events))
	for _, event := range events {
		items = append(items, liveSnapshotResponse{
			ProviderSymbol:   event.ProviderSymbol,
			EventType:        event.EventType,
			ObservedAt:       event.ObservedAt.UTC().Format(time.RFC3339Nano),
			Price:            requiredNumeric(event.Price),
			Quantity:         requiredNumeric(event.Quantity),
			SourceReceivedAt: event.SourceReceivedAt.UTC().Format(time.RFC3339Nano),
		})
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{"data": items})
}
