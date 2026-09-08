package httpapi

import (
	// net/http provides the response writer and status code constants.
	"net/http"
	// time formats status timestamps consistently with the snapshot endpoint.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// liveMonitorStatusResponse is the public JSON contract for operational live
// monitoring metadata. Nullable timestamps stay null until the first event.
type liveMonitorStatusResponse struct {
	Enabled                   bool    `json:"enabled"`
	Provider                  string  `json:"provider"`
	ProviderSymbol            string  `json:"provider_symbol"`
	State                     string  `json:"state"`
	Received                  int64   `json:"received"`
	Accepted                  int64   `json:"accepted"`
	Rejected                  int64   `json:"rejected"`
	LastEventObservedAt       *string `json:"last_event_observed_at"`
	LastEventSourceReceivedAt *string `json:"last_event_source_received_at"`
	LastError                 *string `json:"last_error"`
	UpdatedAt                 string  `json:"updated_at"`
}

// liveMonitorStatusHandler exposes monitor lifecycle information without
// exposing internal mutexes or Go time values directly to API clients.
func liveMonitorStatusHandler(
	responseWriter http.ResponseWriter,
	statusStore *ingestion.LiveMonitorStatusStore,
) {
	if statusStore == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "live monitor status store is not configured",
		})
		return
	}

	status := statusStore.Snapshot()
	response := liveMonitorStatusResponse{
		Enabled:        status.Enabled,
		Provider:       status.Provider,
		ProviderSymbol: status.ProviderSymbol,
		State:          status.State,
		Received:       status.Received,
		Accepted:       status.Accepted,
		Rejected:       status.Rejected,
		UpdatedAt:      status.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if status.LastEventObservedAt != nil {
		formatted := status.LastEventObservedAt.UTC().Format(time.RFC3339Nano)
		response.LastEventObservedAt = &formatted
	}
	if status.LastEventSourceReceivedAt != nil {
		formatted := status.LastEventSourceReceivedAt.UTC().Format(time.RFC3339Nano)
		response.LastEventSourceReceivedAt = &formatted
	}
	if status.LastError != "" {
		lastError := status.LastError
		response.LastError = &lastError
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{"data": response})
}
