package httpapi

import (
	// log records detailed repository failures for the backend operator.
	"log"
	// net/http provides request and response types plus HTTP status constants.
	"net/http"
	// strings normalizes symbols/providers and helps build exact decimal strings.
	"strings"
	// time parses the RFC3339 timestamps supplied by API clients.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

// marketCandleResponse is the public JSON representation of one OHLCV candle.
//
// Decimal prices and volumes are returned as strings deliberately. Converting
// PostgreSQL NUMERIC values to float64 could round large or highly precise
// values, which would be unsafe for financial data. Clients can parse these
// strings with a decimal library when arithmetic is required.
type marketCandleResponse struct {
	ID                  int64   `json:"id"`
	Interval            string  `json:"interval"`
	ObservedAt          string  `json:"observed_at"`
	SourceCloseAt       *string `json:"source_close_at"`
	Open                string  `json:"open"`
	High                string  `json:"high"`
	Low                 string  `json:"low"`
	Close               string  `json:"close"`
	Volume              string  `json:"volume"`
	QuoteVolume         *string `json:"quote_volume"`
	TradeCount          *int64  `json:"trade_count"`
	TakerBuyVolume      *string `json:"taker_buy_volume"`
	TakerBuyQuoteVolume *string `json:"taker_buy_quote_volume"`
	SourceRetrievedAt   string  `json:"source_retrieved_at"`
}

// listCandlesHandler serves candles for one symbol, provider, interval, and
// UTC time range.
//
// Required query parameters:
//   - symbol: canonical instrument symbol, such as BTCUSDT or SBIN
//   - provider: source name, such as binance or yahoo
//   - interval: supported interval, such as 1d
//   - from: inclusive RFC3339 timestamp
//   - to: exclusive RFC3339 timestamp
//
// The handler validates client input before calling PostgreSQL. This keeps
// malformed requests as HTTP 400 responses instead of turning them into vague
// database errors.
func listCandlesHandler(
	responseWriter http.ResponseWriter,
	request *http.Request,
	marketCandleRepository *database.MarketCandleRepository,
) {
	// A missing repository means the server was wired incorrectly. It is an
	// internal failure, so clients receive 500 rather than a validation error.
	if marketCandleRepository == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "market candle repository is not configured",
		})
		return
	}

	query := request.URL.Query()
	symbol := strings.ToUpper(strings.TrimSpace(query.Get("symbol")))
	provider := strings.ToLower(strings.TrimSpace(query.Get("provider")))
	interval := strings.TrimSpace(query.Get("interval"))
	fromText := strings.TrimSpace(query.Get("from"))
	toText := strings.TrimSpace(query.Get("to"))

	// Check all required text values together so the client receives one clear
	// message instead of discovering missing parameters one request at a time.
	if symbol == "" || provider == "" || interval == "" || fromText == "" || toText == "" {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{
			"error": "symbol, provider, interval, from, and to are required",
		})
		return
	}

	// The database schema supports these intervals. Rejecting unsupported values
	// at the API boundary gives a predictable 400 response before an unnecessary
	// database query is made.
	if !supportedCandleIntervals[interval] {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{
			"error": "unsupported interval",
		})
		return
	}

	from, err := parseRFC3339QueryTime("from", fromText)
	if err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	to, err := parseRFC3339QueryTime("to", toText)
	if err != nil {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// A half-open range must move forward. The repository uses >= from and < to,
	// so equal or reversed timestamps would otherwise produce a confusing empty
	// result rather than clearly identifying the request problem.
	if !from.Before(to) {
		writeJSON(responseWriter, http.StatusBadRequest, map[string]string{
			"error": "to must be after from",
		})
		return
	}

	// pgtype.Timestamptz preserves the UTC instant and explicitly marks the value
	// as valid for the generated sqlc parameter struct.
	fromTimestamp := pgtype.Timestamptz{Time: from, Valid: true}
	toTimestamp := pgtype.Timestamptz{Time: to, Valid: true}
	candles, err := marketCandleRepository.ListByCanonicalSymbol(
		request.Context(),
		symbol,
		provider,
		interval,
		fromTimestamp,
		toTimestamp,
	)
	if err != nil {
		// Keep database details in server logs and return a stable public message.
		log.Printf("list candles for %s via %s: %v", symbol, provider, err)
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "unable to load market candles",
		})
		return
	}

	// Convert every database model into the public API contract. An empty slice
	// remains [] in JSON, which is easier for frontend code than a null value.
	items := make([]marketCandleResponse, 0, len(candles))
	for _, candle := range candles {
		items = append(items, marketCandleResponse{
			ID:                  candle.ID,
			Interval:            candle.Interval,
			ObservedAt:          formatTimestamp(candle.ObservedAt),
			SourceCloseAt:       optionalTimestamp(candle.SourceCloseAt),
			Open:                requiredNumeric(candle.Open),
			High:                requiredNumeric(candle.High),
			Low:                 requiredNumeric(candle.Low),
			Close:               requiredNumeric(candle.Close),
			Volume:              requiredNumeric(candle.Volume),
			QuoteVolume:         optionalNumeric(candle.QuoteVolume),
			TradeCount:          optionalInt8(candle.TradeCount),
			TakerBuyVolume:      optionalNumeric(candle.TakerBuyVolume),
			TakerBuyQuoteVolume: optionalNumeric(candle.TakerBuyQuoteVolume),
			SourceRetrievedAt:   formatTimestamp(candle.SourceRetrievedAt),
		})
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{
		"data": items,
	})
}

// supportedCandleIntervals mirrors the database constraint and makes interval
// validation a constant-time lookup instead of a long conditional expression.
var supportedCandleIntervals = map[string]bool{
	"1m":  true,
	"5m":  true,
	"15m": true,
	"30m": true,
	"1h":  true,
	"4h":  true,
	"1d":  true,
	"1w":  true,
}

// parseRFC3339QueryTime parses one API timestamp and normalizes it to UTC.
// RFC3339 accepts values such as 2026-08-01T00:00:00Z and values with explicit
// offsets. Normalizing here gives every database query one consistent timezone.
func parseRFC3339QueryTime(parameterName, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, &queryParameterError{
			parameter: parameterName,
			message:   "must be an RFC3339 timestamp",
		}
	}
	return parsed.UTC(), nil
}

// queryParameterError keeps malformed timestamp messages consistent without
// exposing the lower-level parser wording to API clients.
type queryParameterError struct {
	parameter string
	message   string
}

func (err *queryParameterError) Error() string {
	return err.parameter + " " + err.message
}

// formatTimestamp converts a valid PostgreSQL timestamp to an ISO-8601 UTC
// string. The repository marks required timestamps as valid by schema contract.
func formatTimestamp(value pgtype.Timestamptz) string {
	return value.Time.UTC().Format(time.RFC3339Nano)
}

// optionalTimestamp maps SQL NULL timestamps to JSON null and valid timestamps
// to pointers, allowing encoding/json to distinguish absent from present data.
func optionalTimestamp(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	formatted := formatTimestamp(value)
	return &formatted
}

// requiredNumeric returns an exact decimal string for a required PostgreSQL
// NUMERIC value. The ingestion validation and database constraints guarantee
// these fields are valid, so an invalid value is represented as an empty string
// rather than silently converting it to an inaccurate floating-point number.
func requiredNumeric(value pgtype.Numeric) string {
	formatted, ok := numericString(value)
	if !ok {
		return ""
	}
	return formatted
}

// optionalNumeric maps a nullable PostgreSQL NUMERIC value to a nullable JSON
// string. Binance-specific metrics therefore appear as null for Yahoo candles.
func optionalNumeric(value pgtype.Numeric) *string {
	formatted, ok := numericString(value)
	if !ok {
		return nil
	}
	return &formatted
}

// numericString reconstructs pgtype.Numeric's integer/exponent representation
// without converting through float64. For example, Int=273 and Exp=-2 becomes
// "2.73". It returns false for SQL NULL, NaN, and infinity values.
func numericString(value pgtype.Numeric) (string, bool) {
	if !value.Valid || value.NaN || value.InfinityModifier != pgtype.Finite || value.Int == nil {
		return "", false
	}

	digits := value.Int.String()
	negative := strings.HasPrefix(digits, "-")
	if negative {
		digits = strings.TrimPrefix(digits, "-")
	}

	var formatted string
	if value.Exp >= 0 {
		formatted = digits + strings.Repeat("0", int(value.Exp))
	} else {
		decimalPosition := len(digits) + int(value.Exp)
		switch {
		case decimalPosition > 0:
			formatted = digits[:decimalPosition] + "." + digits[decimalPosition:]
		case decimalPosition <= 0:
			formatted = "0." + strings.Repeat("0", -decimalPosition) + digits
		}
	}

	if negative && formatted != "0" {
		formatted = "-" + formatted
	}
	return formatted, true
}

// optionalInt8 maps pgtype.Int8 to *int64 so SQL NULL becomes JSON null.
func optionalInt8(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	converted := value.Int64
	return &converted
}
