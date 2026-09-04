// Package main contains the executable entry point for the MoneyPlant API.
// In Go, an executable program must use package main. The compiled program
// begins execution in the main function below.
package main

import (
	// context and time give the startup database check a finite deadline.
	"context"
	// errors lets shutdown distinguish the expected http.ErrServerClosed value
	// from an unexpected server failure.
	"errors"
	// The standard-library log package writes timestamped messages to the terminal.
	// We use it here for startup and fatal configuration messages.
	"log"
	// net/http provides the expected error returned when a server is shut down.
	"net/http"
	// os and os/signal allow the process to wait for Ctrl+C or a termination signal.
	"os"
	"os/signal"
	// syscall provides SIGTERM, the standard graceful-termination signal used
	// by Docker and most process managers.
	"syscall"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/httpapi"
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

	// Phase 6.2 update: build the instrument repository from the already-open
	// shared pool. Repositories do not create extra pools; they reuse PostgreSQL
	// connections managed by databasePool and are then injected into the HTTP
	// server that needs them.
	instrumentRepository := database.NewInstrumentRepository(databasePool)
	// Phase 6.2 update: create the market-candle repository from the same shared
	// pool so API candle reads use the database layer already used by ingestion.
	marketCandleRepository := database.NewMarketCandleRepository(databasePool)

	// Phase 6.1 update: construct the HTTP server after configuration and database
	// startup have succeeded. This ordering prevents the API from accepting
	// requests while a required backend dependency is unavailable.
	apiServer := httpapi.NewServer(cfg.APIHost, cfg.APIPort, instrumentRepository, marketCandleRepository)

	// Phase 6.1 update: run ListenAndServe in a goroutine so main can wait for
	// either a server failure or an operating-system shutdown signal. A buffered
	// channel lets the server goroutine report its result without being stuck if
	// the signal path wins the select first.
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("MoneyPlant HTTP API listening on %s", apiServer.Addr)
		serverErrors <- apiServer.ListenAndServe()
	}()

	// Phase 6.1 update: subscribe to SIGINT (Ctrl+C) and SIGTERM (normal process
	// termination). signal.Notify delivers those OS events to this channel so the
	// process can finish active requests before closing its resources.
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdownSignals)

	select {
	case err := <-serverErrors:
		// http.Server returns ErrServerClosed during an intentional shutdown. Any
		// other error means the server stopped unexpectedly and should be reported.
		// We continue to the shared shutdown block so the database pool and any
		// remaining HTTP resources still follow the normal cleanup path.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
		}
	case shutdownSignal := <-shutdownSignals:
		log.Printf("shutdown signal received: %s", shutdownSignal)
	}

	// Phase 6.1 update: give active HTTP requests five seconds to finish before
	// forcefully closing connections. Shutdown does not accept new requests and
	// returns once existing handlers complete or the deadline expires.
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := apiServer.Shutdown(shutdownContext); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}
}
