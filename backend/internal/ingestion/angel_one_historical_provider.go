package ingestion

import (
	// context carries cancellation through every historical page request.
	"context"
	// encoding/json preserves Angel One's decimal text before PostgreSQL parsing.
	"encoding/json"
	// fmt creates safe operation and row-level normalization errors.
	"fmt"
	// net/http performs the authenticated SmartAPI request.
	"net/http"
	// net/url validates the configured SmartAPI endpoint.
	"net/url"
	// strings normalizes symbols, response messages, and JSON scalar values.
	"strings"
	// time handles the provider's IST request format and normalized UTC output.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	angelOneHistoricalPath           = "/rest/secure/angelbroking/historical/v1/getCandleData"
	angelOneHistoricalTimeout        = 15 * time.Second
	angelOneHistoricalExchange       = "NSE"
	angelOneHistoricalLocationOffset = 5*60*60 + 30*60
)

// AngelOneHistoricalMarketDataProvider fetches normalized candles from the
// authenticated SmartAPI historical endpoint. The session is deliberately
// supplied by the caller and remains in memory; callers can Login again after
// an expired session without persisting provider tokens.
type AngelOneHistoricalMarketDataProvider struct {
	client        *http.Client
	authenticator *AngelOneAuthenticator
	session       AngelOneSession
}

// NewAngelOneHistoricalMarketDataProvider creates an authenticated Angel One
// historical adapter. A valid session normally comes from Authenticator.Login.
// The HTTP client is injectable so all provider tests remain offline.
func NewAngelOneHistoricalMarketDataProvider(
	client *http.Client,
	authenticator *AngelOneAuthenticator,
	session AngelOneSession,
) (*AngelOneHistoricalMarketDataProvider, error) {
	if authenticator == nil {
		return nil, fmt.Errorf("Angel One authenticator cannot be nil")
	}
	if strings.TrimSpace(session.JWTToken()) == "" {
		return nil, fmt.Errorf("Angel One historical provider requires a JWT session")
	}
	if client == nil {
		client = &http.Client{Timeout: angelOneHistoricalTimeout}
	}
	return &AngelOneHistoricalMarketDataProvider{
		client:        client,
		authenticator: authenticator,
		session:       session,
	}, nil
}

// ProviderName identifies Angel One in ingestion provenance and audit rows.
func (p *AngelOneHistoricalMarketDataProvider) ProviderName() string {
	return string(ProviderAngelOne)
}

// Capabilities lists the SmartAPI intervals supported by this adapter. Angel
// One does not provide the normalized 4h or 1w values directly, so those are
// intentionally rejected instead of being synthesized silently.
func (p *AngelOneHistoricalMarketDataProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Historical: true,
		SupportedIntervals: []MarketInterval{
			Interval1m,
			Interval5m,
			Interval15m,
			Interval30m,
			Interval1h,
			Interval1d,
		},
	}
}

// FetchHistoricalCandles retrieves a half-open [From, To) range. SmartAPI
// limits each interval to a maximum number of calendar days, so the adapter
// splits large requests before calling the provider and filters each response
// locally against the exact UTC window.
func (p *AngelOneHistoricalMarketDataProvider) FetchHistoricalCandles(
	ctx context.Context,
	request HistoricalCandleRequest,
) ([]database.MarketCandleInput, error) {
	if err := validateAngelOneHistoricalRequest(request); err != nil {
		return nil, err
	}

	retrievedAt := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	from := request.From.Time.UTC()
	to := request.To.Time.UTC()
	maxDays := angelOneIntervalPolicy[request.Interval].maxDays
	seen := make(map[time.Time]struct{})
	candles := make([]database.MarketCandleInput, 0)

	for from.Before(to) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		pageTo := from.AddDate(0, 0, maxDays)
		if pageTo.After(to) {
			pageTo = to
		}
		pageRequest := request
		pageRequest.From = pgtype.Timestamptz{Time: from, Valid: true}
		pageRequest.To = pgtype.Timestamptz{Time: pageTo, Valid: true}

		rows, err := p.fetchHistoricalPage(ctx, pageRequest)
		if err != nil {
			return nil, err
		}
		for rowIndex, row := range rows {
			candle, err := normalizeAngelOneCandle(row, request.Interval, retrievedAt)
			if err != nil {
				return nil, fmt.Errorf("normalize Angel One candle row %d: %w", rowIndex, err)
			}
			observedAt := candle.ObservedAt.Time.UTC()
			if observedAt.Before(from) || !observedAt.Before(pageTo) {
				continue
			}
			if _, exists := seen[observedAt]; exists {
				continue
			}
			seen[observedAt] = struct{}{}
			candles = append(candles, candle)
		}

		from = pageTo
	}

	return candles, nil
}

type angelOneHistoricalIntervalPolicy struct {
	providerInterval string
	maxDays          int
}

var angelOneIntervalPolicy = map[string]angelOneHistoricalIntervalPolicy{
	"1m":  {providerInterval: "ONE_MINUTE", maxDays: 30},
	"5m":  {providerInterval: "FIVE_MINUTE", maxDays: 100},
	"15m": {providerInterval: "FIFTEEN_MINUTE", maxDays: 200},
	"30m": {providerInterval: "THIRTY_MINUTE", maxDays: 200},
	"1h":  {providerInterval: "ONE_HOUR", maxDays: 400},
	"1d":  {providerInterval: "ONE_DAY", maxDays: 2000},
}

type angelOneHistoricalRequest struct {
	Exchange    string `json:"exchange"`
	SymbolToken string `json:"symboltoken"`
	Interval    string `json:"interval"`
	FromDate    string `json:"fromdate"`
	ToDate      string `json:"todate"`
}

type angelOneHistoricalResponse struct {
	Status    bool                `json:"status"`
	Message   string              `json:"message"`
	ErrorCode string              `json:"errorcode"`
	Data      [][]json.RawMessage `json:"data"`
}

func (p *AngelOneHistoricalMarketDataProvider) fetchHistoricalPage(
	ctx context.Context,
	request HistoricalCandleRequest,
) ([][]json.RawMessage, error) {
	endpoint, err := url.Parse(p.authenticator.baseURL + angelOneHistoricalPath)
	if err != nil {
		return nil, fmt.Errorf("build Angel One historical URL: %w", err)
	}

	policy := angelOneIntervalPolicy[request.Interval]
	location := time.FixedZone("IST", angelOneHistoricalLocationOffset)
	payload := angelOneHistoricalRequest{
		Exchange:    angelOneHistoricalExchange,
		SymbolToken: strings.TrimSpace(request.ProviderInstrumentID),
		Interval:    policy.providerInterval,
		FromDate:    request.From.Time.In(location).Format("2006-01-02 15:04"),
		ToDate:      request.To.Time.In(location).Format("2006-01-02 15:04"),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal Angel One historical request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create Angel One historical request: %w", err)
	}
	p.authenticator.setHeaders(req, p.session.JWTToken())

	response, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request Angel One historical candles: %w", err)
	}
	defer response.Body.Close()

	var envelope angelOneHistoricalResponse
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode Angel One historical response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || !envelope.Status {
		message := safeProviderMessage(envelope.Message, p.authenticator.credentials.APIKey, p.session.JWTToken())
		if envelope.ErrorCode != "" {
			return nil, fmt.Errorf("Angel One historical request failed with HTTP %d (%s): %s", response.StatusCode, envelope.ErrorCode, message)
		}
		return nil, fmt.Errorf("Angel One historical request failed with HTTP %d: %s", response.StatusCode, message)
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("Angel One historical response contains no candle data")
	}
	return envelope.Data, nil
}

func validateAngelOneHistoricalRequest(request HistoricalCandleRequest) error {
	if strings.TrimSpace(request.ProviderInstrumentID) == "" {
		return fmt.Errorf("Angel One provider instrument ID cannot be empty")
	}
	if _, supported := angelOneIntervalPolicy[request.Interval]; !supported {
		return fmt.Errorf("unsupported Angel One interval %q", request.Interval)
	}
	if !request.From.Valid || !request.To.Valid {
		return fmt.Errorf("Angel One historical requests require valid From and To timestamps")
	}
	if !request.From.Time.Before(request.To.Time) {
		return fmt.Errorf("Angel One request To must be after From")
	}
	return nil
}

func normalizeAngelOneCandle(
	row []json.RawMessage,
	interval string,
	retrievedAt pgtype.Timestamptz,
) (database.MarketCandleInput, error) {
	if len(row) != 6 {
		return database.MarketCandleInput{}, fmt.Errorf("expected 6 fields, got %d", len(row))
	}
	observedAt, err := angelOneTimestamp(row[0])
	if err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("timestamp: %w", err)
	}
	values := make([]pgtype.Numeric, 5)
	for index, field := range []string{"open", "high", "low", "close", "volume"} {
		values[index], err = angelOneNumeric(row[index+1], field)
		if err != nil {
			return database.MarketCandleInput{}, err
		}
	}
	return database.MarketCandleInput{
		Interval:          interval,
		ObservedAt:        observedAt,
		Open:              values[0],
		High:              values[1],
		Low:               values[2],
		Close:             values[3],
		Volume:            values[4],
		SourceRetrievedAt: retrievedAt,
	}, nil
}

func angelOneTimestamp(raw json.RawMessage) (pgtype.Timestamptz, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return pgtype.Timestamptz{}, fmt.Errorf("timestamp must be an RFC3339 string: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return pgtype.Timestamptz{}, fmt.Errorf("parse %q: %w", value, err)
	}
	return pgtype.Timestamptz{Time: parsed.UTC(), Valid: true}, nil
}

func angelOneNumeric(raw json.RawMessage, fieldName string) (pgtype.Numeric, error) {
	valueText := strings.TrimSpace(string(raw))
	if valueText == "" || valueText == "null" {
		return pgtype.Numeric{}, fmt.Errorf("Angel One %s value is null or empty", fieldName)
	}
	if strings.HasPrefix(valueText, "\"") {
		var quotedValue string
		if err := json.Unmarshal(raw, &quotedValue); err != nil {
			return pgtype.Numeric{}, fmt.Errorf("decode Angel One %s decimal: %w", fieldName, err)
		}
		valueText = quotedValue
	}
	var numericValue pgtype.Numeric
	if err := numericValue.Scan(valueText); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("parse Angel One %s value %q: %w", fieldName, valueText, err)
	}
	return numericValue, nil
}
