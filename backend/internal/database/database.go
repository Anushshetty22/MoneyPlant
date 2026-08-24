// Package database contains PostgreSQL connection and repository infrastructure.
// Phase 3.1 update: this package was introduced to move PostgreSQL startup
// details out of main.go and create the foundation for later repositories.
// Keeping connection creation here prevents the rest of the backend from
// needing to know how PostgreSQL URLs, pools, or health checks are configured.
package database

import (
	// context carries a deadline and cancellation signal through the connection attempt.
	"context"
	// fmt adds useful operation names to returned errors.
	"fmt"
	// net joins a host and port correctly for IPv4 and IPv6 addresses.
	"net"
	// net/url safely escapes usernames and passwords when building the PostgreSQL URL.
	"net/url"
	// strconv converts the integer port from Config into text for the network address.
	"strconv"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates and verifies a PostgreSQL connection pool.
//
// The flow is:
//  1. Build a connection string from the already-validated application config.
//  2. Parse that string into pgxpool configuration.
//  3. Create the pool, which manages reusable database connections.
//  4. Ping PostgreSQL to confirm the database is reachable now, not only later
//     when the first query is executed.
//  5. Return the ready pool to main.go so other packages can use it.
//
// If any step fails, the function returns an error. When a pool was created but
// the health check fails, it is closed before returning so connections do not leak.
func NewPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	// Build the URL separately so connection-string construction is isolated from
	// pool creation and can be reasoned about independently.
	connectionString := buildConnectionString(cfg)

	// ParseConfig validates the PostgreSQL URL and creates the pool settings that
	// pgxpool.NewWithConfig will use to manage connections.
	poolConfig, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}

	// NewWithConfig creates a pool rather than one connection. A pool allows future
	// API requests to reuse connections efficiently and safely across goroutines.
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL connection pool: %w", err)
	}

	// Ping sends a lightweight request to PostgreSQL. This turns a configuration
	// or networking problem into a clear startup error before the HTTP server starts.
	if err := pool.Ping(ctx); err != nil {
		// The pool is no longer useful after a failed health check, so release it
		// before returning the error to main.go.
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	// At this point the caller owns the pool and must call pool.Close() during shutdown.
	return pool, nil
}

// buildConnectionString converts the typed Config values into a PostgreSQL URL.
// url.URL and url.UserPassword perform escaping for special characters in
// credentials, so a password containing characters such as @ or : does not
// corrupt the connection string. This function does not log or print the URL.
func buildConnectionString(cfg config.Config) string {
	connectionURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.PostgresUser, cfg.PostgresPassword),
		Host:   net.JoinHostPort(cfg.PostgresHost, strconv.Itoa(cfg.PostgresPort)),
		Path:   "/" + cfg.PostgresDatabase,
	}

	return connectionURL.String()
}
