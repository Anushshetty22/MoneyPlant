package httpapi

import (
	// log records detailed repository errors for backend operators.
	"log"
	// net/http provides request and response types plus HTTP status constants.
	"net/http"
	// strings normalizes dataset codes and removes accidental surrounding spaces.
	"strings"
	// time parses and formats calendar dates and retrieval timestamps.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

// macroDatasetResponse is the public JSON shape for one macro series definition.
// Metadata is kept out of this first response because it may contain source-
// specific implementation details that are not required for dataset selection.
type macroDatasetResponse struct {
	ID              int64   `json:"id"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	Provider        string  `json:"provider"`
	Metric          string  `json:"metric"`
	Unit            string  `json:"unit"`
	Frequency       string  `json:"frequency"`
	ObservationType string  `json:"observation_type"`
	BasePeriod      *string `json:"base_period"`
	SourceURL       string  `json:"source_url"`
	RetrievedAt     string  `json:"retrieved_at"`
	IsActive        bool    `json:"is_active"`
}

// macroObservationResponse is the public JSON shape for one dated macro value.
// Value is a string for the same precision reason as market-candle decimals.
type macroObservationResponse struct {
	ID                 int64   `json:"id"`
	ObservedOn         string  `json:"observed_on"`
	Value              string  `json:"value"`
	SourceRetrievedAt  string  `json:"source_retrieved_at"`
	SourceRowReference *string `json:"source_row_reference"`
}

// listMacroDatasetsHandler returns active macro dataset definitions.
// Dataset definitions tell the frontend what a series means before it requests
// the corresponding values—for example, that CPI is measured in percent and
// published monthly.
func listMacroDatasetsHandler(
	responseWriter http.ResponseWriter,
	request *http.Request,
	macroDatasetRepository *database.MacroDatasetRepository,
) {
	// A nil repository indicates incorrect server wiring and therefore produces
	// a server error rather than a panic.
	if macroDatasetRepository == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "macro dataset repository is not configured",
		})
		return
	}

	// Use the request context so a disconnected client can cancel the database
	// query and release the shared pool connection promptly.
	datasets, err := macroDatasetRepository.ListActive(request.Context())
	if err != nil {
		log.Printf("list macro datasets: %v", err)
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "unable to load macro datasets",
		})
		return
	}

	items := make([]macroDatasetResponse, 0, len(datasets))
	for _, dataset := range datasets {
		items = append(items, macroDatasetResponse{
			ID:              dataset.ID,
			Code:            dataset.Code,
			Name:            dataset.Name,
			Provider:        dataset.Provider,
			Metric:          dataset.Metric,
			Unit:            dataset.Unit,
			Frequency:       dataset.Frequency,
			ObservationType: dataset.ObservationType,
			BasePeriod:      dataset.BasePeriod,
			SourceURL:       dataset.SourceURL,
			RetrievedAt:     formatTimestamp(dataset.RetrievedAt),
			IsActive:        dataset.IsActive,
		})
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{"data": items})
}

// listMacroObservationsHandler returns values for one macro dataset.
//
// The dataset code is required. from and to are optional as a pair:
//   - neither supplied: return the complete stored series
//   - both supplied: return observed_on >= from and observed_on < to
//   - only one supplied: reject the request because the range is incomplete
func listMacroObservationsHandler(
	responseWriter http.ResponseWriter,
	request *http.Request,
	macroObservationRepository *database.MacroObservationRepository,
) {
	if macroObservationRepository == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "macro observation repository is not configured",
		})
		return
	}

	query := request.URL.Query()
	code := strings.ToLower(strings.TrimSpace(query.Get("dataset")))
	fromText := strings.TrimSpace(query.Get("from"))
	toText := strings.TrimSpace(query.Get("to"))
	if code == "" {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{
			"error": "dataset is required",
		})
		return
	}
	if (fromText == "") != (toText == "") {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{
			"error": "from and to must be provided together",
		})
		return
	}

	var observations []database.MacroObservation
	var err error
	if fromText == "" {
		// Without a range, use the existing full-series repository method.
		observations, err = macroObservationRepository.ListByDatasetCode(request.Context(), code)
	} else {
		from, parseErr := parseMacroDate("from", fromText)
		if parseErr != nil {
			writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": parseErr.Error()})
			return
		}
		to, parseErr := parseMacroDate("to", toText)
		if parseErr != nil {
			writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": parseErr.Error()})
			return
		}
		if !from.Before(to) {
			writeJSON(responseWriter, http.StatusBadRequest, map[string]string{
				"error": "to must be after from",
			})
			return
		}

		// A SQL DATE represents a calendar day, so pass pgtype.Date rather than
		// inventing a timezone or time of day for the database query.
		observations, err = macroObservationRepository.ListByDatasetCodeInRange(
			request.Context(),
			code,
			pgtype.Date{Time: from, Valid: true},
			pgtype.Date{Time: to, Valid: true},
		)
	}
	if err != nil {
		log.Printf("list macro observations for %s: %v", code, err)
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "unable to load macro observations",
		})
		return
	}

	items := make([]macroObservationResponse, 0, len(observations))
	for _, observation := range observations {
		items = append(items, macroObservationResponse{
			ID:                 observation.ID,
			ObservedOn:         formatMacroDate(observation.ObservedOn),
			Value:              requiredNumeric(observation.Value),
			SourceRetrievedAt:  formatTimestamp(observation.SourceRetrievedAt),
			SourceRowReference: observation.SourceRowReference,
		})
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{"data": items})
}

// parseMacroDate accepts the API's date-only format and normalizes the parsed
// value to UTC for predictable pgtype.Date construction.
func parseMacroDate(parameterName, value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, &queryParameterError{
			parameter: parameterName,
			message:   "must be a YYYY-MM-DD date",
		}
	}
	return parsed.UTC(), nil
}

// formatMacroDate serializes PostgreSQL DATE as ISO-8601 without adding a time.
func formatMacroDate(value pgtype.Date) string {
	return value.Time.UTC().Format("2006-01-02")
}
