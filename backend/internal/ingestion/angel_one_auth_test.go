package ingestion_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

func TestGenerateTOTPMatchesRFC6238SHA1Vector(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := ingestion.GenerateTOTP(secret, time.Unix(59, 0).UTC())
	if err != nil {
		t.Fatalf("GenerateTOTP() error = %v", err)
	}
	if code != "287082" {
		t.Fatalf("TOTP = %s, want 287082", code)
	}
}

func TestAngelOneAuthenticatorLoginAndRefreshKeepTokensInMemory(t *testing.T) {
	const (
		apiKey       = "test-api-key"
		clientCode   = "TEST123"
		password     = "test-pin"
		totpSecret   = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
		jwtToken     = "jwt-token-value"
		refreshToken = "refresh-token-value"
		feedToken    = "feed-token-value"
	)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/rest/auth/angelbroking/user/v1/loginByPassword" {
			if request.Header.Get("X-PrivateKey") != apiKey || request.Header.Get("X-UserType") != "USER" || request.Header.Get("X-SourceID") != "WEB" {
				t.Fatalf("login headers missing SmartAPI values: %#v", request.Header)
			}
			var payload map[string]string
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode login payload: %v", err)
			}
			if payload["clientcode"] != clientCode || payload["password"] != password || payload["totp"] != "287082" {
				t.Fatalf("login payload = %#v", payload)
			}
			writeSessionResponse(response, jwtToken, refreshToken, feedToken)
			return
		}

		if request.URL.Path == "/rest/auth/angelbroking/jwt/v1/generateTokens" {
			if request.Header.Get("Authorization") != "Bearer "+jwtToken {
				t.Fatalf("refresh Authorization = %q", request.Header.Get("Authorization"))
			}
			var payload map[string]string
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode refresh payload: %v", err)
			}
			if payload["refreshToken"] != refreshToken {
				t.Fatalf("refresh payload = %#v", payload)
			}
			writeSessionResponse(response, "refreshed-jwt", "refreshed-refresh", "refreshed-feed")
			return
		}

		http.NotFound(response, request)
	}))
	defer server.Close()

	authenticator, err := ingestion.NewAngelOneAuthenticatorWithSettingsAndClock(
		nil,
		ingestion.AngelOneAuthSettings{BaseURL: server.URL},
		ingestion.AngelOneCredentials{
			APIKey: apiKey, ClientCode: clientCode, Password: password, TOTPSecret: totpSecret,
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
	if session.JWTToken() != "Bearer "+jwtToken || session.RefreshToken() != refreshToken || session.FeedToken() != feedToken {
		t.Fatalf("login session = %s", session)
	}
	if strings.Contains(fmt.Sprintf("%#v", session), jwtToken) || strings.Contains(session.String(), refreshToken) {
		t.Fatal("session formatting exposed a token")
	}

	refreshed, err := authenticator.Refresh(context.Background(), session)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.JWTToken() != "Bearer refreshed-jwt" || refreshed.RefreshToken() != "refreshed-refresh" || refreshed.FeedToken() != "refreshed-feed" {
		t.Fatalf("refreshed session = %s", refreshed)
	}
}

func TestAngelOneAuthenticatorReturnsSafeInvalidCredentialError(t *testing.T) {
	const secret = "test-api-key"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"status":false,"message":"invalid test-api-key","errorcode":"AB1007","data":null}`))
	}))
	defer server.Close()

	authenticator, err := ingestion.NewAngelOneAuthenticator(
		nil,
		server.URL,
		ingestion.AngelOneCredentials{
			APIKey: secret, ClientCode: "TEST123", Password: "test-pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		},
	)
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}

	_, err = authenticator.Login(context.Background())
	if err == nil {
		t.Fatal("login error = nil, want invalid credential error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error exposed API key: %v", err)
	}
	if !strings.Contains(err.Error(), "AB1007") || !strings.Contains(err.Error(), "redacted") {
		t.Fatalf("error = %v, want safe provider context", err)
	}
}

func TestAngelOneAuthenticatorClassifiesExpiredRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/auth/angelbroking/user/v1/loginByPassword" {
			writeSessionResponse(response, "jwt", "refresh", "feed")
			return
		}
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"status":false,"message":"refresh token expired","errorcode":"AB8051","data":null}`))
	}))
	defer server.Close()

	authenticator, err := ingestion.NewAngelOneAuthenticator(
		nil,
		server.URL,
		ingestion.AngelOneCredentials{
			APIKey: "api-key", ClientCode: "TEST123", Password: "test-pin", TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		},
	)
	if err != nil {
		t.Fatalf("create authenticator: %v", err)
	}

	session, err := authenticator.Login(context.Background())
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_, err = authenticator.Refresh(context.Background(), session)
	if !ingestion.IsAngelOneSessionExpired(err) {
		t.Fatalf("expired error = %v, want expired classification", err)
	}
}

func writeSessionResponse(response http.ResponseWriter, jwtToken, refreshToken, feedToken string) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(response, `{"status":true,"message":"SUCCESS","errorcode":"","data":{"jwtToken":%q,"refreshToken":%q,"feedToken":%q}}`, jwtToken, refreshToken, feedToken)
}
