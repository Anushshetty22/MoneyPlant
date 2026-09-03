// Package httpapi contains the HTTP transport layer for the MoneyPlant backend.
// It owns routes, request/response formatting, middleware, and server settings.
// Database repositories will be connected to handlers in later Phase 6 steps.
package httpapi

import (
	// encoding/json converts Go response values into JSON for API clients.
	"encoding/json"
	// log records each request after the handler has finished processing it.
	"log"
	// net/http provides the HTTP server, request type, response writer, and
	// standard status-code constants used by this package.
	"net/http"
	// net joins the host and port safely, including IPv6 host values.
	"net"
	// strconv converts the configured integer port into text for the server address.
	"strconv"
	// time measures request duration and limits how long the server waits for
	// clients that send incomplete or unusually slow requests.
	"time"
)

// NewServer creates the configured HTTP server used by cmd/api/main.go.
//
// The construction flow is intentionally separate from server startup:
//  1. Create a standard-library ServeMux and register the routes.
//  2. Wrap the mux with request logging middleware.
//  3. Return an http.Server with safe timeout settings.
//
// Keeping construction separate makes the server easy to test without opening
// a real network port and gives later phases one place to add API routes.
func NewServer(host string, port int) *http.Server {
	// ServeMux maps an incoming HTTP method and path to a handler function.
	// The health route is the first endpoint because it gives us a small,
	// dependency-free way to confirm that the API process is reachable.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler)

	// The middleware surrounds every registered route. This means future routes
	// automatically receive the same request logging behavior without repeating
	// logging code inside every handler.
	loggedHandler := requestLoggingMiddleware(mux)

	// The timeout settings protect the process from clients that connect but do
	// not finish sending headers or request bodies. They also prevent a handler
	// from holding a server connection forever during a slow response.
	return &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           loggedHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// healthHandler returns a small JSON response when the API process is alive.
//
// This first health check confirms HTTP availability only. Database-aware
// health checks will be added when the read API handlers and database queries
// are wired together in Phase 6.2.
func healthHandler(responseWriter http.ResponseWriter, request *http.Request) {
	// The content type tells curl, browsers, and frontend clients how to interpret
	// the response body. Setting it before WriteHeader ensures it is sent with the
	// status line rather than being added too late.
	responseWriter.Header().Set("Content-Type", "application/json")

	// Encode writes the response object as JSON and adds a newline. A map keeps
	// this first response intentionally small; a typed response struct can be
	// introduced when the public API response contract expands.
	writeJSON(responseWriter, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

// writeJSON centralizes successful and error response serialization.
// Handlers call this helper instead of manually setting headers and encoding
// values, which keeps response behavior consistent across all API endpoints.
func writeJSON(responseWriter http.ResponseWriter, statusCode int, value any) {
	// Set the status before encoding so the client receives the intended HTTP
	// status even when the value is an error response.
	responseWriter.WriteHeader(statusCode)

	// JSON encoding can fail for unsupported Go values. Current API responses use
	// strings, structs, and slices that are JSON-safe; if a future handler passes
	// an unsupported value, return a plain internal-server-error body rather than
	// exposing an encoding error or leaving the client without a response.
	if err := json.NewEncoder(responseWriter).Encode(value); err != nil {
		log.Printf("encode JSON response: %v", err)
	}
}

// requestLoggingMiddleware records the method, path, status, and duration for
// every request. The wrapped responseWriter captures the status code because
// net/http's default ResponseWriter does not expose the code after writing.
func requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		statusRecorder := &responseStatusRecorder{
			ResponseWriter: responseWriter,
			statusCode:     http.StatusOK,
		}

		// Passing the recorder to the next handler allows it to preserve the
		// original response behavior while remembering the final status code.
		next.ServeHTTP(statusRecorder, request)

		log.Printf(
			"HTTP %s %s status=%d duration=%s",
			request.Method,
			request.URL.Path,
			statusRecorder.statusCode,
			time.Since(startedAt),
		)
	})
}

// responseStatusRecorder decorates http.ResponseWriter with the status code
// observed by the logging middleware. Header, Write, and WriteHeader are
// forwarded so handlers continue to use it exactly like a normal writer.
type responseStatusRecorder struct {
	http.ResponseWriter
	statusCode int
	headerSent bool
}

// WriteHeader remembers the first status code and then forwards it to the
// underlying writer. HTTP permits only the first WriteHeader call to decide the
// response status, so later calls are deliberately ignored for logging too.
func (recorder *responseStatusRecorder) WriteHeader(statusCode int) {
	if recorder.headerSent {
		return
	}
	recorder.statusCode = statusCode
	recorder.headerSent = true
	recorder.ResponseWriter.WriteHeader(statusCode)
}

// Write forwards the response body and ensures an implicit 200 status is
// recorded when a handler writes a body without calling WriteHeader first.
func (recorder *responseStatusRecorder) Write(body []byte) (int, error) {
	if !recorder.headerSent {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.ResponseWriter.Write(body)
}
