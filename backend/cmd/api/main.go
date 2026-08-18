// Package main contains the executable entry point for the MoneyPlant API.
package main

import (
	"log"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
)

// main is the first function executed when the API program starts.
func main() {
	// Load and validate configuration before starting any application component.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	// Log connection details needed for debugging, but never print the database password.
	log.Printf(
		"MoneyPlant API starting on %s:%d; PostgreSQL target %s:%d/%s as user %s",
		cfg.APIHost,
		cfg.APIPort,
		cfg.PostgresHost,
		cfg.PostgresPort,
		cfg.PostgresDatabase,
		cfg.PostgresUser,
	)
}
