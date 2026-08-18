// Package config loads and validates runtime configuration for the API.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config contains the settings required to start the MoneyPlant API.
// Keeping configuration in one struct avoids reading environment variables throughout the application.
type Config struct {
	APIHost string
	APIPort int

	PostgresHost     string
	PostgresPort     int
	PostgresDatabase string
	PostgresUser     string
	PostgresPassword string
}

// Load reads configuration from environment variables and applies local-development defaults.
func Load() (Config, error) {
	// Read the API settings used later by the HTTP server.
	apiPort, err := getInt("API_PORT", 8080)
	if err != nil {
		return Config{}, err
	}

	// Read the PostgreSQL settings used later by the database connection layer.
	postgresPort, err := getInt("POSTGRES_PORT", 5432)
	if err != nil {
		return Config{}, err
	}

	config := Config{
		APIHost:          getString("API_HOST", "0.0.0.0"),
		APIPort:          apiPort,
		PostgresHost:     getString("POSTGRES_HOST", "localhost"),
		PostgresPort:     postgresPort,
		PostgresDatabase: getString("POSTGRES_DB", "moneyplant"),
		PostgresUser:     getString("POSTGRES_USER", "moneyplant"),
		PostgresPassword: getString("POSTGRES_PASSWORD", "change-me-locally"),
	}

	// Validate ports before any server or database component tries to use them.
	if err := validatePort("API_PORT", config.APIPort); err != nil {
		return Config{}, err
	}
	if err := validatePort("POSTGRES_PORT", config.PostgresPort); err != nil {
		return Config{}, err
	}

	return config, nil
}

// getString returns an environment value or a safe local-development default.
func getString(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
}

// getInt reads an integer environment value and returns a useful configuration error when parsing fails.
func getInt(key string, fallback int) (int, error) {
	value := getString(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

// validatePort prevents invalid network-port values from reaching the server or database driver.
func validatePort(key string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", key)
	}
	return nil
}
