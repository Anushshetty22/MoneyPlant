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
	CanonicalSymbol           string  `json:"canonical_symbol"`
	ProviderSymbol            string  `json:"provider_symbol"`
	State                     string  `json:"state"`
	Received                  int64   `json:"received"`
	Accepted                  int64   `json:"accepted"`
	Rejected                  int64   `json:"rejected"`
	Reconnects                int64   `json:"reconnects"`
	Persisted                 int64   `json:"persisted"`
	Restored                  int64   `json:"restored"`
	LastEventObservedAt       *string `json:"last_event_observed_at"`
	LastEventSourceReceivedAt *string `json:"last_event_source_received_at"`
	LastPersistedAt           *string `json:"last_persisted_at"`
	LastError                 *string `json:"last_error"`
	LastReconnectError        *string `json:"last_reconnect_error"`
	LastPersistenceError      *string `json:"last_persistence_error"`
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
		Enabled:         status.Enabled,
		Provider:        status.Provider,
		CanonicalSymbol: status.CanonicalSymbol,
		ProviderSymbol:  status.ProviderSymbol,
		State:           status.State,
		Received:        status.Received,
		Accepted:        status.Accepted,
		Rejected:        status.Rejected,
		Reconnects:      status.Reconnects,
		Persisted:       status.Persisted,
		Restored:        status.Restored,
		UpdatedAt:       status.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if status.LastEventObservedAt != nil {
		formatted := status.LastEventObservedAt.UTC().Format(time.RFC3339Nano)
		response.LastEventObservedAt = &formatted
	}
	if status.LastEventSourceReceivedAt != nil {
		formatted := status.LastEventSourceReceivedAt.UTC().Format(time.RFC3339Nano)
		response.LastEventSourceReceivedAt = &formatted
	}
	if status.LastPersistedAt != nil {
		formatted := status.LastPersistedAt.UTC().Format(time.RFC3339Nano)
		response.LastPersistedAt = &formatted
	}
	if status.LastError != "" {
		lastError := status.LastError
		response.LastError = &lastError
	}
	if status.LastReconnectError != "" {
		lastReconnectError := status.LastReconnectError
		response.LastReconnectError = &lastReconnectError
	}
	if status.LastPersistenceError != "" {
		lastPersistenceError := status.LastPersistenceError
		response.LastPersistenceError = &lastPersistenceError
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{"data": response})
}
