package ingestion

import (
	// context carries cancellation and deadlines through authentication calls.
	"context"
	// crypto/hmac and crypto/sha1 implement the TOTP algorithm used by the
	// authenticator secret without adding a third-party dependency.
	"crypto/hmac"
	"crypto/sha1"
	// encoding/base32 decodes the base32 secret supplied by Angel One.
	"encoding/base32"
	// encoding/binary encodes the TOTP moving counter as required by RFC 6238.
	"encoding/binary"
	// encoding/json encodes login requests and decodes the provider envelope.
	"encoding/json"
	// errors supports context cancellation checks and wrapped transport errors.
	"errors"
	// fmt creates safe operation-level errors without including credentials or tokens.
	"fmt"
	// io limits response reads and distinguishes a complete response from a
	// truncated provider payload.
	"io"
	// net/http performs the HTTPS requests and is injectable for tests.
	"net/http"
	// net/url validates and joins the configured provider base URL.
	"net/url"
	// strings normalizes URLs, headers, token prefixes, and safe error messages.
	"strings"
	// sync protects the injectable clock/configuration boundary if future callers
	// reuse one authenticator concurrently.
	"sync"
	// time supplies TOTP windows, request timestamps, and the default HTTP timeout.
	"time"
)

const (
	// AngelOneDefaultBaseURL is the SmartAPI host documented by Angel One.
	AngelOneDefaultBaseURL = "https://apiconnect.angelone.in"

	angelOneLoginPath   = "/rest/auth/angelbroking/user/v1/loginByPassword"
	angelOneRefreshPath = "/rest/auth/angelbroking/jwt/v1/generateTokens"
	angelOneHTTPTimeout = 15 * time.Second
	angelOneMaxBodySize = 64 * 1024
)

// AngelOneCredentials contains the four local secrets required to create a
// SmartAPI session. Keep this value in memory only; do not serialize or log it.
type AngelOneCredentials struct {
	APIKey     string
	ClientCode string
	Password   string
	TOTPSecret string
}

// Validate checks presence and format without revealing any credential value in
// the returned error. The TOTP secret is validated as base32 because that is
// the format emitted by authenticator-app enrollment flows.
func (c AngelOneCredentials) Validate() error {
	missing := make([]string, 0, 4)
	if strings.TrimSpace(c.APIKey) == "" {
		missing = append(missing, "API key")
	}
	if strings.TrimSpace(c.ClientCode) == "" {
		missing = append(missing, "client code")
	}
	if strings.TrimSpace(c.Password) == "" {
		missing = append(missing, "password")
	}
	if strings.TrimSpace(c.TOTPSecret) == "" {
		missing = append(missing, "TOTP secret")
	}
	if len(missing) > 0 {
		return fmt.Errorf("Angel One credentials missing: %s", strings.Join(missing, ", "))
	}
	if _, err := decodeTOTPSecret(c.TOTPSecret); err != nil {
		return fmt.Errorf("Angel One TOTP secret is invalid: %w", err)
	}
	return nil
}

// AngelOneAuthSettings contains non-secret HTTP settings for the session
// client. The request metadata fields match SmartAPI's documented headers.
type AngelOneAuthSettings struct {
	BaseURL        string
	ClientLocalIP  string
	ClientPublicIP string
	MACAddress     string
}

// AngelOneSession holds the three provider-issued tokens in memory. The fields
// remain private so accidental JSON or fmt output cannot expose them.
type AngelOneSession struct {
	jwtToken     string
	refreshToken string
	feedToken    string
	obtainedAt   time.Time
}

// JWTToken returns the normalized bearer token for authenticated HTTP calls.
func (s AngelOneSession) JWTToken() string { return s.jwtToken }

// RefreshToken returns the provider refresh token for a future refresh call.
func (s AngelOneSession) RefreshToken() string { return s.refreshToken }

// FeedToken returns the provider feed token for the later WebSocket phase.
func (s AngelOneSession) FeedToken() string { return s.feedToken }

// ObtainedAt records when the local process accepted the provider response.
func (s AngelOneSession) ObtainedAt() time.Time { return s.obtainedAt }

// String intentionally omits all token values so normal logging remains safe.
func (s AngelOneSession) String() string {
	if s.obtainedAt.IsZero() {
		return "AngelOneSession{empty}"
	}
	return fmt.Sprintf("AngelOneSession{obtained_at:%s}", s.obtainedAt.UTC().Format(time.RFC3339))
}

// GoString keeps %#v diagnostics redacted as well as ordinary %s logging.
func (s AngelOneSession) GoString() string { return s.String() }

// AngelOneAuthError describes an authentication failure without retaining the
// request body, response body, API key, password, TOTP, or token values.
type AngelOneAuthError struct {
	Operation  string
	StatusCode int
	ErrorCode  string
	Message    string
	Expired    bool
}

func (e *AngelOneAuthError) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := "Angel One " + e.Operation + " failed"
	if e.StatusCode != 0 {
		message += fmt.Sprintf(" with HTTP %d", e.StatusCode)
	}
	if e.ErrorCode != "" {
		message += " (" + e.ErrorCode + ")"
	}
	if e.Message != "" {
		message += ": " + e.Message
	}
	return message
}

// IsAngelOneSessionExpired reports whether the provider rejected a JWT or
// refresh token because it expired. Callers can then perform a fresh Login.
func IsAngelOneSessionExpired(err error) bool {
	var authErr *AngelOneAuthError
	return errors.As(err, &authErr) && authErr.Expired
}

// AngelOneAuthenticator creates and refreshes SmartAPI sessions. It does not
// persist tokens or log request/response bodies.
type AngelOneAuthenticator struct {
	client      *http.Client
	baseURL     string
	credentials AngelOneCredentials
	settings    AngelOneAuthSettings
	now         func() time.Time
	mu          sync.Mutex
}

// NewAngelOneAuthenticator creates a production authenticator with a bounded
// HTTP client. Callers should load credentials from environment variables.
func NewAngelOneAuthenticator(
	client *http.Client,
	baseURL string,
	credentials AngelOneCredentials,
) (*AngelOneAuthenticator, error) {
	return NewAngelOneAuthenticatorWithSettings(client, AngelOneAuthSettings{BaseURL: baseURL}, credentials)
}

// NewAngelOneAuthenticatorWithSettings allows deployments to supply the
// provider's required request metadata and allows tests to use a local server.
func NewAngelOneAuthenticatorWithSettings(
	client *http.Client,
	settings AngelOneAuthSettings,
	credentials AngelOneCredentials,
) (*AngelOneAuthenticator, error) {
	return NewAngelOneAuthenticatorWithSettingsAndClock(client, settings, credentials, time.Now)
}

// NewAngelOneAuthenticatorWithSettingsAndClock is the deterministic constructor
// used by tests to verify TOTP boundaries without waiting for wall-clock time.
func NewAngelOneAuthenticatorWithSettingsAndClock(
	client *http.Client,
	settings AngelOneAuthSettings,
	credentials AngelOneCredentials,
	now func() time.Time,
) (*AngelOneAuthenticator, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	baseURL := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if baseURL == "" {
		baseURL = AngelOneDefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("invalid Angel One base URL")
	}
	if client == nil {
		client = &http.Client{Timeout: angelOneHTTPTimeout}
	}
	if settings.ClientLocalIP == "" {
		settings.ClientLocalIP = "127.0.0.1"
	}
	if settings.ClientPublicIP == "" {
		settings.ClientPublicIP = "127.0.0.1"
	}
	if settings.MACAddress == "" {
		settings.MACAddress = "00:00:00:00:00:00"
	}

	return &AngelOneAuthenticator{
		client:      client,
		baseURL:     baseURL,
		credentials: credentials,
		settings:    settings,
		now:         now,
	}, nil
}

// Login creates a fresh session using the current six-digit TOTP code.
func (a *AngelOneAuthenticator) Login(ctx context.Context) (AngelOneSession, error) {
	if a == nil {
		return AngelOneSession{}, fmt.Errorf("Angel One authenticator is nil")
	}
	if err := ctx.Err(); err != nil {
		return AngelOneSession{}, err
	}

	a.mu.Lock()
	now := a.now()
	a.mu.Unlock()
	otp, err := GenerateTOTP(a.credentials.TOTPSecret, now)
	if err != nil {
		return AngelOneSession{}, fmt.Errorf("generate Angel One TOTP: %w", err)
	}

	payload := struct {
		ClientCode string `json:"clientcode"`
		Password   string `json:"password"`
		TOTP       string `json:"totp"`
	}{a.credentials.ClientCode, a.credentials.Password, otp}
	return a.postSessionRequest(ctx, angelOneLoginPath, "login", payload, "", otp)
}

// Refresh obtains a new JWT and feed token from a valid refresh token. An
// expired refresh token is returned as a typed error so the caller can Login.
func (a *AngelOneAuthenticator) Refresh(ctx context.Context, session AngelOneSession) (AngelOneSession, error) {
	if a == nil {
		return AngelOneSession{}, fmt.Errorf("Angel One authenticator is nil")
	}
	if strings.TrimSpace(session.RefreshToken()) == "" {
		return AngelOneSession{}, fmt.Errorf("Angel One refresh token is empty")
	}
	payload := struct {
		RefreshToken string `json:"refreshToken"`
	}{session.RefreshToken()}
	return a.postSessionRequest(ctx, angelOneRefreshPath, "refresh", payload, session.JWTToken(), session.RefreshToken())
}

func (a *AngelOneAuthenticator) postSessionRequest(ctx context.Context, path, operation string, payload any, authorization string, sensitiveValues ...string) (AngelOneSession, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return AngelOneSession{}, fmt.Errorf("marshal Angel One %s request: %w", operation, err)
	}
	endpoint := a.baseURL + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return AngelOneSession{}, fmt.Errorf("create Angel One %s request: %w", operation, err)
	}
	a.setHeaders(request, authorization)

	response, err := a.client.Do(request)
	if err != nil {
		return AngelOneSession{}, fmt.Errorf("request Angel One %s: %w", operation, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, angelOneMaxBodySize))
	if err != nil {
		return AngelOneSession{}, fmt.Errorf("read Angel One %s response: %w", operation, err)
	}

	var envelope angelOneSessionEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return AngelOneSession{}, &AngelOneAuthError{
			Operation:  operation,
			StatusCode: response.StatusCode,
			Message:    "provider returned an unreadable response",
		}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || !envelope.Status {
		return AngelOneSession{}, authErrorFromEnvelope(operation, response.StatusCode, envelope, append(sensitiveValues, a.credentials.APIKey, a.credentials.ClientCode, a.credentials.Password, a.credentials.TOTPSecret, authorization)...)
	}
	if envelope.Data == nil || envelope.Data.JWTToken == "" || envelope.Data.RefreshToken == "" || envelope.Data.FeedToken == "" {
		return AngelOneSession{}, &AngelOneAuthError{
			Operation:  operation,
			StatusCode: response.StatusCode,
			Message:    "provider response did not contain all session tokens",
		}
	}

	a.mu.Lock()
	now := a.now()
	a.mu.Unlock()
	return AngelOneSession{
		jwtToken:     normalizeBearerToken(envelope.Data.JWTToken),
		refreshToken: envelope.Data.RefreshToken,
		feedToken:    envelope.Data.FeedToken,
		obtainedAt:   now.UTC(),
	}, nil
}

func (a *AngelOneAuthenticator) setHeaders(request *http.Request, authorization string) {
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-UserType", "USER")
	request.Header.Set("X-SourceID", "WEB")
	request.Header.Set("X-ClientLocalIP", a.settings.ClientLocalIP)
	request.Header.Set("X-ClientPublicIP", a.settings.ClientPublicIP)
	request.Header.Set("X-MACAddress", a.settings.MACAddress)
	request.Header.Set("X-PrivateKey", a.credentials.APIKey)
	if authorization != "" {
		request.Header.Set("Authorization", normalizeBearerToken(authorization))
	}
}

type angelOneSessionEnvelope struct {
	Status    bool                      `json:"status"`
	Message   string                    `json:"message"`
	ErrorCode string                    `json:"errorcode"`
	Data      *angelOneSessionTokenData `json:"data"`
}

type angelOneSessionTokenData struct {
	JWTToken     string `json:"jwtToken"`
	RefreshToken string `json:"refreshToken"`
	FeedToken    string `json:"feedToken"`
}

func authErrorFromEnvelope(operation string, statusCode int, envelope angelOneSessionEnvelope, sensitiveValues ...string) *AngelOneAuthError {
	errorCode := strings.TrimSpace(envelope.ErrorCode)
	return &AngelOneAuthError{
		Operation:  operation,
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Message:    safeProviderMessage(envelope.Message, sensitiveValues...),
		Expired:    errorCode == "AG8002" || errorCode == "AB8051",
	}
}

func safeProviderMessage(message string, sensitiveValues ...string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "provider rejected the request"
	}
	for _, sensitive := range sensitiveValues {
		if strings.TrimSpace(sensitive) != "" {
			message = strings.ReplaceAll(message, sensitive, "<redacted>")
		}
	}
	return message
}

func normalizeBearerToken(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return "Bearer " + strings.TrimSpace(token[len("Bearer "):])
	}
	return "Bearer " + token
}

// GenerateTOTP implements the six-digit SHA-1 time-based one-time password
// algorithm used by authenticator applications. The secret is never returned.
func GenerateTOTP(secret string, now time.Time) (string, error) {
	decoded, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	counter := uint64(now.Unix() / 30)
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)
	hash := hmac.New(sha1.New, decoded)
	_, _ = hash.Write(counterBytes[:])
	sum := hash.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000), nil
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	cleaned := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	if cleaned == "" {
		return nil, fmt.Errorf("TOTP secret is empty")
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(cleaned, "="))
	if err != nil {
		return nil, fmt.Errorf("TOTP secret must be base32")
	}
	return decoded, nil
}
