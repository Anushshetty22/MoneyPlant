// Package config loads and validates runtime configuration for the API.
// Keeping this work in its own package gives main.go one clear entry point and
// prevents database or HTTP packages from reading environment variables directly.
package config

import (
	// fmt creates errors that include the configuration key and the reason it failed.
	"fmt"
	// os reads environment variables supplied by the shell or process manager.
	"os"
	// strconv converts text such as "5432" into the integer required by network code.
	"strconv"
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

	// Assemble all successfully parsed values into one configuration object.
	// getString checks the environment first and uses the fallback when the
	// variable is missing or empty, making local development easy while still
	// allowing deployment environments to provide their own values.
	config := Config{
		APIHost:          getString("API_HOST", "0.0.0.0"),
		APIPort:          apiPort,
		PostgresHost:     getString("POSTGRES_HOST", "localhost"),
		PostgresPort:     postgresPort,
		PostgresDatabase: getString("POSTGRES_DB", "moneyplant"),
		PostgresUser:     getString("POSTGRES_USER", "moneyplant"),
		PostgresPassword: getString("POSTGRES_PASSWORD", "change-me-locally"),
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

// validatePort prevents invalid network-port values from reaching the server or database driver.
// Valid TCP/UDP ports occupy the inclusive range 1 through 65535; zero and
// negative values cannot represent a usable network port.
func validatePort(key string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", key)
	}
	return nil
}
