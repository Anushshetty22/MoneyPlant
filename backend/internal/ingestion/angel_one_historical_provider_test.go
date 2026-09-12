package ingestion_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestAngelOneHistoricalProviderMapsAndSplitsCandles(t *testing.T) {
	const baseURL = "https://angel.test"
	requestCount := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/rest/auth/angelbroking/user/v1/loginByPassword" {
			return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":{"jwtToken":"jwt","refreshToken":"refresh","feedToken":"feed"}}`), nil
		}
		if request.URL.Path != "/rest/secure/angelbroking/historical/v1/getCandleData" {
			return jsonResponse(http.StatusNotFound, `{"status":false,"message":"not found","errorcode":"","data":null}`), nil
		}
		requestCount++
		if request.Header.Get("Authorization") != "Bearer jwt" || request.Header.Get("X-PrivateKey") != "test-api-key" {
			t.Errorf("authenticated headers missing: authorization=%q private-key=%q", request.Header.Get("Authorization"), request.Header.Get("X-PrivateKey"))
		}
		var payload struct {
			Exchange    string `json:"exchange"`
			SymbolToken string `json:"symboltoken"`
			Interval    string `json:"interval"`
			FromDate    string `json:"fromdate"`
			ToDate      string `json:"todate"`
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		if payload.Exchange != "NSE" || payload.SymbolToken != "99926000" || payload.Interval != "ONE_MINUTE" {
			t.Errorf("historical request payload = %#v", payload)
		}
		if requestCount == 1 {
			if payload.FromDate != "2026-01-01 05:30" || payload.ToDate != "2026-01-31 05:30" {
				t.Errorf("first request dates = %q to %q", payload.FromDate, payload.ToDate)
			}
			return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":[["2026-01-01T05:30:00+05:30","19571.20","19573.35","19534.40","19552.05","1000"]]}`), nil
		}
		if payload.FromDate != "2026-01-31 05:30" || payload.ToDate != "2026-02-01 05:30" {
			t.Errorf("second request dates = %q to %q", payload.FromDate, payload.ToDate)
		}
		return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":[["2026-01-31T05:30:00+05:30",19552.05,19560.00,19500.00,19540.00,1100]]}`), nil
	})
	client := &http.Client{Transport: transport}
	authenticator, err := ingestion.NewAngelOneAuthenticatorWithSettingsAndClock(
		client,
		ingestion.AngelOneAuthSettings{BaseURL: baseURL},
		ingestion.AngelOneCredentials{
			APIKey: "test-api-key", ClientCode: "TEST123", Password: "test-pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		},
		func() time.Time { return time.Unix(59, 0).UTC() },
	)
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	session, err := authenticator.Login(context.Background())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	provider, err := ingestion.NewAngelOneHistoricalMarketDataProvider(client, authenticator, session)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	from := angelOneTimestampTest(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	to := angelOneTimestampTest(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC))
	candles, err := provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderInstrumentID: "99926000",
		Interval:             "1m",
		From:                 from,
		To:                   to,
	})
	if err != nil {
		t.Fatalf("fetch historical candles: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("historical request count = %d, want 2", requestCount)
	}
	if len(candles) != 2 {
		t.Fatalf("candles = %d, want 2", len(candles))
	}
	if !candles[0].ObservedAt.Time.Equal(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first observed_at = %s", candles[0].ObservedAt.Time)
	}
	if candles[0].Open.Int.String() != "1957120" || candles[0].Open.Exp != -2 {
		t.Errorf("first open = %#v, want exact 19571.20", candles[0].Open)
	}
	if candles[1].Close.Int.String() != "1954000" || candles[1].Close.Exp != -2 {
		t.Errorf("second close = %#v, want exact 19540.00", candles[1].Close)
	}
}

func TestAngelOneHistoricalProviderRejectsMalformedPageWithoutRows(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/rest/auth/angelbroking/user/v1/loginByPassword" {
			return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":{"jwtToken":"jwt","refreshToken":"refresh","feedToken":"feed"}}`), nil
		}
		return jsonResponse(http.StatusOK, `{"status":true,"message":"SUCCESS","errorcode":"","data":[["2026-01-01T00:00:00Z",1,2,3]]}`), nil
	})}
	authenticator, err := ingestion.NewAngelOneAuthenticator(
		client,
		"https://angel.test",
		ingestion.AngelOneCredentials{APIKey: "api", ClientCode: "client", Password: "pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"},
	)
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	session, err := authenticator.Login(context.Background())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	provider, err := ingestion.NewAngelOneHistoricalMarketDataProvider(client, authenticator, session)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	candles, err := provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderInstrumentID: "99926000",
		Interval:             "1d",
		From:                 angelOneTimestampTest(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		To:                   angelOneTimestampTest(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)),
	})
	if err == nil || !strings.Contains(err.Error(), "expected 6 fields") {
		t.Fatalf("error = %v, want malformed-row error", err)
	}
	if candles != nil {
		t.Fatalf("candles = %#v, want nil on malformed page", candles)
	}
}

func TestAngelOneHistoricalProviderRejectsUnsupportedIntervalBeforeHTTP(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, fmt.Errorf("unexpected HTTP request")
	})}
	authenticator, err := ingestion.NewAngelOneAuthenticator(
		client,
		"https://angel.test",
		ingestion.AngelOneCredentials{APIKey: "api", ClientCode: "client", Password: "pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"},
	)
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}
	provider, err := ingestion.NewAngelOneHistoricalMarketDataProvider(client, authenticator, structSession("jwt"))
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	_, err = provider.FetchHistoricalCandles(context.Background(), ingestion.HistoricalCandleRequest{
		ProviderInstrumentID: "99926000",
		Interval:             "4h",
		From:                 angelOneTimestampTest(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		To:                   angelOneTimestampTest(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported Angel One interval") {
		t.Fatalf("error = %v, want unsupported interval error", err)
	}
	if called {
		t.Fatal("unsupported interval made an HTTP request")
	}
}

// structSession uses the public authentication boundary instead of depending
// on token fields that intentionally remain private in the production package.
func structSession(token string) ingestion.AngelOneSession {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, fmt.Sprintf(`{"status":true,"message":"SUCCESS","errorcode":"","data":{"jwtToken":%q,"refreshToken":"refresh","feedToken":"feed"}}`, token)), nil
	})}
	authenticator, _ := ingestion.NewAngelOneAuthenticator(client, "https://session.test", ingestion.AngelOneCredentials{APIKey: "api", ClientCode: "client", Password: "pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"})
	session, _ := authenticator.Login(context.Background())
	return session
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func angelOneTimestampTest(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
