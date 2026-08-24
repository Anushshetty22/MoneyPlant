// Package main contains the executable entry point for the MoneyPlant API.
// In Go, an executable program must use package main. The compiled program
// begins execution in the main function below.
package main

import (
	// The standard-library log package writes timestamped messages to the terminal.
	// We use it here for startup and fatal configuration messages.
	"log"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
)

// main is the first function executed when the API program starts.
// Its job is orchestration: it calls the configuration package, handles a
// startup failure, and then passes the validated configuration to the next
// component. The HTTP server and database connection will be added here later.
func main() {
	// config.Load reads environment variables, applies local defaults, and
	// validates values such as API_PORT and POSTGRES_PORT. It returns two values:
	// a Config struct on success and an error when parsing or validation fails.
	cfg, err := config.Load()
	if err != nil {
		// log.Fatalf prints the error and immediately exits the process with a
		// failure status. Continuing without valid configuration could cause a
		// later database or HTTP error that would be harder to understand.
		log.Fatalf("configuration error: %v", err)
	}

	// At this stage the program only proves that startup configuration is valid.
	// Later code will use cfg to create the database connection and HTTP server.
	// We log safe connection details for debugging, but deliberately exclude
	// cfg.PostgresPassword because secrets must never appear in application logs.
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
