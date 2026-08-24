// Package main contains the executable entry point for the MoneyPlant API.
// In Go, an executable program must use package main. The compiled program
// begins execution in the main function below.
package main

import (
	// context and time give the startup database check a finite deadline.
	"context"
	// The standard-library log package writes timestamped messages to the terminal.
	// We use it here for startup and fatal configuration messages.
	"log"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
)

// main is the first function executed when the API program starts.
// Its job is orchestration: it calls the configuration package, handles a
// startup failure, and then passes the validated configuration to the next
// component. The HTTP server will be added here later.
func main() {
	// Phase 3.1 update: configuration loading was added so the application has
	// one validated source of runtime settings before it creates infrastructure.
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

	// Phase 3.1 update: PostgreSQL startup connectivity was added to verify that
	// the backend can reach the database before later API components start.
	// Create a child context with a five-second deadline for the initial database
	// connection. If PostgreSQL is unavailable, startup fails promptly instead of
	// waiting indefinitely. The deferred cancel releases the context resources.
	databaseContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// database.NewPool parses the connection settings, creates a reusable pgx pool,
	// and pings PostgreSQL. If it returns an error, the API exits before accepting
	// requests because the application cannot work without its database.
	databasePool, err := database.NewPool(databaseContext, cfg)
	if err != nil {
		log.Fatalf("database startup error: %v", err)
	}

	// main now owns the pool. Closing it when the process exits releases database
	// connections cleanly; graceful HTTP shutdown will be added in a later step.
	defer databasePool.Close()

	// Phase 3.1 update: startup logging now reports the validated runtime target.
	// Log safe connection details for debugging, but deliberately exclude the
	// database password because secrets must never appear in application logs.
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
