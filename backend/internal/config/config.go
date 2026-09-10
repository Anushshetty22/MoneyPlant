// Package config loads and validates runtime configuration for the API.
// Keeping this work in its own package gives main.go one clear entry point and
// prevents database or HTTP packages from reading environment variables directly.
package config

import (
	// fmt creates errors that include the configuration key and the reason it failed.
	"fmt"
	// net/url validates the configured Binance WebSocket endpoint before startup.
	"net/url"
	// os reads environment variables supplied by the shell or process manager.
	"os"
	// strconv converts text such as "5432" into the integer required by network code.
	"strconv"
	// strings trims operator-provided symbols before validating their characters.
	"strings"
	// time represents live-monitor windows with explicit units such as 5s.
	"time"
)

// Config contains the settings required to start the MoneyPlant API.
// Keeping configuration in one struct avoids reading environment variables
// throughout the application. Other packages receive this already-parsed
// object instead of knowing the names or formats of environment variables.
type Config struct {
	// APIHost and APIPort will tell the HTTP server which network address to bind.
	APIHost string
	APIPort int

	// These fields will be used by the database package to build a PostgreSQL connection.
	PostgresHost     string
	PostgresPort     int
	PostgresDatabase string
	PostgresUser     string
	PostgresPassword string

	// Live monitoring is opt-in. An empty symbol keeps the API in its existing
	// behavior without opening a network stream. LiveMonitorSymbol remains for
	// backward compatibility; LiveMonitorSymbols is the Phase 3 multi-symbol
	// representation.
	LiveMonitorSymbol       string
	LiveMonitorSymbols      []string
	LiveMonitorMaxRetries   int
	LiveMonitorWebSocketURL string
	// LiveMonitorPersistenceInterval limits how often the open-ended stream
	// writes the latest value to PostgreSQL.
	LiveMonitorPersistenceInterval time.Duration
	// LiveMonitorFinalPersistenceTimeout bounds the final shutdown save.
	LiveMonitorFinalPersistenceTimeout time.Duration
	// LiveMonitorRestoreTimeout bounds startup restoration before HTTP serving.
	LiveMonitorRestoreTimeout time.Duration

	// Angel One credentials are read only from the process environment. They are
	// passed to the authentication client but are never persisted in Config logs
	// or database records by the application.
	AngelOneAPIKey         string
	AngelOneClientCode     string
	AngelOnePassword       string
	AngelOneTOTPSecret     string
	AngelOneBaseURL        string
	AngelOneClientLocalIP  string
	AngelOneClientPublicIP string
	AngelOneMACAddress     string
}

// Load reads configuration from environment variables and applies local-development defaults.
// The flow is sequential: parse numeric values first, assemble one Config value,
// validate the assembled values, and return it to main.go. If any step fails,
// the zero Config and the error travel back to main.go, which stops startup
// before another component uses invalid settings.
func Load() (Config, error) {
	// Read API_PORT through getInt because environment variables are always text,
	// while the future HTTP server needs an integer port. A parsing error returns
	// immediately, so the rest of the configuration is not used accidentally.
	apiPort, err := getInt("API_PORT", 8080)
	if err != nil {
		return Config{}, err
	}

	// Read POSTGRES_PORT using the same path. Keeping parsing in one helper makes
	// API and database port behavior consistent and avoids duplicated conversion logic.
	postgresPort, err := getInt("POSTGRES_PORT", 5432)
	if err != nil {
		return Config{}, err
	}
	liveMonitorMaxRetries, err := getInt("LIVE_MONITOR_MAX_RETRIES", 3)
	if err != nil {
		return Config{}, err
	}
	if liveMonitorMaxRetries < 0 {
		return Config{}, fmt.Errorf("LIVE_MONITOR_MAX_RETRIES cannot be negative")
	}
	liveMonitorPersistenceInterval, err := getDuration("LIVE_MONITOR_PERSIST_INTERVAL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	liveMonitorFinalPersistenceTimeout, err := getDuration("LIVE_MONITOR_FINAL_PERSIST_TIMEOUT", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	liveMonitorRestoreTimeout, err := getDuration("LIVE_MONITOR_RESTORE_TIMEOUT", 3*time.Second)
	if err != nil {
		return Config{}, err
	}

	configuredSymbol := getString("LIVE_MONITOR_SYMBOL", "")
	configuredSymbols, err := parseLiveMonitorSymbols(os.Getenv("LIVE_MONITOR_SYMBOLS"), configuredSymbol)
	if err != nil {
		return Config{}, err
	}
	if len(configuredSymbols) > 0 {
		configuredSymbol = configuredSymbols[0]
	}

	// Assemble all successfully parsed values into one configuration object.
	// getString checks the environment first and uses the fallback when the
	// variable is missing or empty, making local development easy while still
	// allowing deployment environments to provide their own values.
	config := Config{
		APIHost:                            getString("API_HOST", "0.0.0.0"),
		APIPort:                            apiPort,
		PostgresHost:                       getString("POSTGRES_HOST", "localhost"),
		PostgresPort:                       postgresPort,
		PostgresDatabase:                   getString("POSTGRES_DB", "moneyplant"),
		PostgresUser:                       getString("POSTGRES_USER", "moneyplant"),
		PostgresPassword:                   getString("POSTGRES_PASSWORD", "change-me-locally"),
		LiveMonitorSymbol:                  configuredSymbol,
		LiveMonitorSymbols:                 configuredSymbols,
		LiveMonitorMaxRetries:              liveMonitorMaxRetries,
		LiveMonitorWebSocketURL:            getString("LIVE_MONITOR_WS_URL", "wss://stream.binance.com:9443"),
		LiveMonitorPersistenceInterval:     liveMonitorPersistenceInterval,
		LiveMonitorFinalPersistenceTimeout: liveMonitorFinalPersistenceTimeout,
		LiveMonitorRestoreTimeout:          liveMonitorRestoreTimeout,
		AngelOneAPIKey:                     getString("ANGEL_ONE_API_KEY", ""),
		AngelOneClientCode:                 getString("ANGEL_ONE_CLIENT_CODE", ""),
		AngelOnePassword:                   getString("ANGEL_ONE_PASSWORD", ""),
		AngelOneTOTPSecret:                 getString("ANGEL_ONE_TOTP_SECRET", ""),
		AngelOneBaseURL:                    getString("ANGEL_ONE_BASE_URL", "https://apiconnect.angelone.in"),
		AngelOneClientLocalIP:              getString("ANGEL_ONE_CLIENT_LOCAL_IP", "127.0.0.1"),
		AngelOneClientPublicIP:             getString("ANGEL_ONE_CLIENT_PUBLIC_IP", "127.0.0.1"),
		AngelOneMACAddress:                 getString("ANGEL_ONE_MAC_ADDRESS", "00:00:00:00:00:00"),
	}

	// Validate the completed configuration before returning it. This is the final
	// gate in Load: if both ports are valid, main.go can safely move on to creating
	// the database pool and HTTP server in later steps.
	if err := validatePort("API_PORT", config.APIPort); err != nil {
		return Config{}, err
	}
	if err := validatePort("POSTGRES_PORT", config.PostgresPort); err != nil {
		return Config{}, err
	}
	if err := validatePositiveDuration("LIVE_MONITOR_PERSIST_INTERVAL", config.LiveMonitorPersistenceInterval); err != nil {
		return Config{}, err
	}
	if err := validatePositiveDuration("LIVE_MONITOR_FINAL_PERSIST_TIMEOUT", config.LiveMonitorFinalPersistenceTimeout); err != nil {
		return Config{}, err
	}
	if err := validatePositiveDuration("LIVE_MONITOR_RESTORE_TIMEOUT", config.LiveMonitorRestoreTimeout); err != nil {
		return Config{}, err
	}
	if err := validateWebSocketURL(config.LiveMonitorWebSocketURL); err != nil {
		return Config{}, err
	}
	return config, nil
}

// getString returns an environment value or a safe local-development default.
// os.LookupEnv distinguishes a missing variable from a present-but-empty one.
// For this project both cases use the fallback, preventing empty connection values.
func getString(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
}

// getInt reads an integer environment value and returns a useful configuration error when parsing fails.
// It gets text through getString, then strconv.Atoi converts that text. The
// wrapped error preserves the original parsing reason for debugging.
func getInt(key string, fallback int) (int, error) {
	value := getString(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

// getDuration reads a Go duration such as "500ms" or "5s". Requiring an
// explicit unit prevents an ambiguous integer from silently meaning seconds in
// one setting and milliseconds in another.
func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := getString(key, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 5s: %w", key, err)
	}
	return parsed, nil
}

// validatePort prevents invalid network-port values from reaching the server or database driver.
// Valid TCP/UDP ports occupy the inclusive range 1 through 65535; zero and
// negative values cannot represent a usable network port.
func validatePort(key string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", key)
	}
	return nil
}

func validatePositiveDuration(key string, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("%s must be positive", key)
	}
	return nil
}

func validateWebSocketURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("LIVE_MONITOR_WS_URL must be a valid WebSocket URL")
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return fmt.Errorf("LIVE_MONITOR_WS_URL must use ws or wss")
	}
	return nil
}

func validateLiveMonitorSymbol(value string) error {
	return validateLiveMonitorSymbolForKey("LIVE_MONITOR_SYMBOL", value)
}

func validateLiveMonitorSymbolForKey(key, value string) error {
	symbol := strings.TrimSpace(value)
	if symbol == "" {
		return nil
	}
	for _, character := range symbol {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') {
			return fmt.Errorf("%s %q contains unsupported character %q", key, value, character)
		}
	}
	return nil
}

// parseLiveMonitorSymbols gives the plural setting precedence while keeping
// LIVE_MONITOR_SYMBOL working for existing local commands and documentation.
// Empty entries are rejected so a typo such as "BTCUSDT,,ETHUSDT" cannot
// silently create an unnamed monitor.
func parseLiveMonitorSymbols(pluralValue, singularValue string) ([]string, error) {
	value := strings.TrimSpace(pluralValue)
	key := "LIVE_MONITOR_SYMBOLS"
	if value == "" {
		value = strings.TrimSpace(singularValue)
		key = "LIVE_MONITOR_SYMBOL"
	}
	if value == "" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		symbol := strings.ToUpper(strings.TrimSpace(part))
		if symbol == "" {
			return nil, fmt.Errorf("%s contains an empty symbol", key)
		}
		if err := validateLiveMonitorSymbolForKey(key, symbol); err != nil {
			return nil, err
		}
		if _, exists := seen[symbol]; exists {
			return nil, fmt.Errorf("%s contains duplicate symbol %q", key, symbol)
		}
		seen[symbol] = struct{}{}
		result = append(result, symbol)
	}
	return result, nil
}
