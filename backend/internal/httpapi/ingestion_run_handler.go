package httpapi

import (
	// encoding/json lets the API return the stored scope as JSON rather than a
	// base64-encoded string, which is what encoding/json would do for []byte.
	"encoding/json"
	// log records detailed repository errors for backend operators.
	"log"
	// net/http provides request and response types plus HTTP status constants.
	"net/http"
	// strconv parses the optional limit query parameter.
	"strconv"
	// strings normalizes the optional provider filter.
	"strings"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
)

// ingestionRunResponse is the public JSON representation of one pipeline run.
// It contains operational information useful for debugging without exposing
// database-specific pgtype values to API clients.
type ingestionRunResponse struct {
	ID            int64           `json:"id"`
	RunType       string          `json:"run_type"`
	Provider      string          `json:"provider"`
	Status        string          `json:"status"`
	StartedAt     string          `json:"started_at"`
	CompletedAt   *string         `json:"completed_at"`
	RequestedFrom *string         `json:"requested_from"`
	RequestedTo   *string         `json:"requested_to"`
	RowsReceived  int64           `json:"rows_received"`
	RowsInserted  int64           `json:"rows_inserted"`
	RowsUpdated   int64           `json:"rows_updated"`
	RowsRejected  int64           `json:"rows_rejected"`
	ErrorMessage  *string         `json:"error_message"`
	Scope         json.RawMessage `json:"scope"`
	CreatedAt     string          `json:"created_at"`
}

// listIngestionRunsHandler returns recent pipeline audit records.
//
// Optional query parameters:
//   - provider: filter by binance, yahoo, rbi_dbie, or another stored provider
//   - limit: number of records, default 20, maximum 100
//
// A bounded default and maximum prevent an accidental API request from loading
// an unreasonably large operational history into one response.
func listIngestionRunsHandler(
	responseWriter http.ResponseWriter,
	request *http.Request,
	ingestionRunRepository *database.IngestionRunRepository,
) {
	if ingestionRunRepository == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "ingestion run repository is not configured",
		})
		return
	}

	query := request.URL.Query()
	provider := strings.ToLower(strings.TrimSpace(query.Get("provider")))
	limit, err := parseRunLimit(query.Get("limit"))
	if err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Select the narrower provider query only when the client asks for one. With
	// no provider, the all-provider query gives the dashboard a complete recent
	// operational view.
	var runs []database.IngestionRun
	if provider == "" {
		runs, err = ingestionRunRepository.ListRecent(request.Context(), limit)
	} else {
		runs, err = ingestionRunRepository.ListRecentByProvider(request.Context(), provider, limit)
	}
	if err != nil {
		log.Printf("list ingestion runs: %v", err)
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "unable to load ingestion history",
		})
		return
	}

	items := make([]ingestionRunResponse, 0, len(runs))
	for _, run := range runs {
		items = append(items, ingestionRunResponse{
			ID:            run.ID,
			RunType:       run.RunType,
			Provider:      run.Provider,
			Status:        run.Status,
			StartedAt:     formatTimestamp(run.StartedAt),
			CompletedAt:   optionalTimestamp(run.CompletedAt),
			RequestedFrom: optionalTimestamp(run.RequestedFrom),
			RequestedTo:   optionalTimestamp(run.RequestedTo),
			RowsReceived:  run.RowsReceived,
			RowsInserted:  run.RowsInserted,
			RowsUpdated:   run.RowsUpdated,
			RowsRejected:  run.RowsRejected,
			ErrorMessage:  run.ErrorMessage,
			Scope:         normalizedJSON(run.Scope),
			CreatedAt:     formatTimestamp(run.CreatedAt),
		})
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{"data": items})
}

// parseRunLimit converts the optional limit into the int32 expected by the
// repository and enforces a small API-level maximum for predictable responses.
func parseRunLimit(value string) (int32, error) {
	if strings.TrimSpace(value) == "" {
		return 20, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 100 {
		return 0, &queryParameterError{
			parameter: "limit",
			message:   "must be an integer between 1 and 100",
		}
	}
	return int32(parsed), nil
}

// normalizedJSON preserves the scope object as JSON. The database default is
// {}, but this fallback also keeps an unexpected empty byte slice valid JSON.
func normalizedJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(value)
}
