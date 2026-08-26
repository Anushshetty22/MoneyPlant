// Command ingest-yahoo downloads Yahoo Finance NSE EOD data and stores it in PostgreSQL.
package main

import (
	// context carries cancellation through configuration, database, HTTP, and repository calls.
	"context"
	// flag provides command-line options without adding a third-party CLI dependency.
	"flag"
	// fmt creates validation errors for command-line timestamps and source resolution.
	"fmt"
	// log prints progress and stops the command with a non-zero exit code on fatal errors.
	"log"
	// strings normalizes the canonical symbol and compares provider names safely.
	"strings"
	// time parses RFC3339 dates and supplies a bounded command timeout.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// main coordinates one user-requested Yahoo Finance fallback batch.
//
// Phase 4.4 update: this command was added to compose the Yahoo adapter with
// the canonical instrument, provider mapping, PostgreSQL repositories, and
// provider-independent MarketIngestionService.
func main() {
	// The command accepts the canonical MoneyPlant symbol, such as SBIN, rather
	// than forcing the user to know Yahoo's provider-specific SBIN.NS symbol.
	// The source mapping table supplies that provider symbol later.
	canonicalSymbol := flag.String("symbol", "SBIN", "canonical MoneyPlant symbol, for example SBIN")
	interval := flag.String("interval", "1d", "candle interval; Yahoo Phase 1 fallback supports 1d")
	fromText := flag.String("from", "", "UTC start timestamp in RFC3339 format, inclusive")
	toText := flag.String("to", "", "UTC end timestamp in RFC3339 format, exclusive")
	flag.Parse()

	from, to, err := parseRequestedRange(*fromText, *toText)
	if err != nil {
		log.Fatalf("invalid requested range: %v", err)
	}

	// Configuration is loaded once at the command boundary. This keeps database
	// and provider packages independent from environment-variable names.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	// The bounded context protects the command from hanging forever while still
	// allowing a small historical EOD request to complete comfortably.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// NewPool creates and verifies the PostgreSQL pool. The deferred Close returns
	// all connections to the operating system when the command finishes.
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		log.Fatalf("database startup error: %v", err)
	}
	defer pool.Close()

	// Resolve the canonical instrument before contacting Yahoo. This prevents
	// accidental ingestion under a misspelled or undefined application symbol.
	instrumentRepository := database.NewInstrumentRepository(pool)
	instrument, err := instrumentRepository.GetByCanonicalSymbol(ctx, strings.ToUpper(strings.TrimSpace(*canonicalSymbol)))
	if err != nil {
		log.Fatalf("resolve instrument %q: %v", *canonicalSymbol, err)
	}

	// Resolve the Yahoo provider mapping from instrument_sources. For SBIN this
	// returns SBIN.NS, which is the identifier Yahoo expects in the chart URL.
	sourceRepository := database.NewInstrumentSourceRepository(pool)
	source, err := findActiveYahooSource(ctx, sourceRepository, instrument.CanonicalSymbol)
	if err != nil {
		log.Fatalf("resolve Yahoo source for %s: %v", instrument.CanonicalSymbol, err)
	}

	// Yahoo's chart endpoint is public for this historical EOD request, so no API
	// key is read or stored by this command.
	provider, err := ingestion.NewYahooMarketDataProvider(nil, ingestion.YahooFinanceChartBaseURL)
	if err != nil {
		log.Fatalf("create Yahoo provider: %v", err)
	}

	// The shared service creates the audit run, fetches normalized rows, assigns
	// instrument_source_id, inserts candles, and completes the audit record.
	service := ingestion.NewMarketIngestionService(
		provider,
		database.NewMarketCandleRepository(pool),
		database.NewIngestionRunRepository(pool),
	)

	result, err := service.IngestHistorical(ctx, instrument.CanonicalSymbol, source.ID, ingestion.HistoricalCandleRequest{
		ProviderSymbol: source.ProviderSymbol,
		Interval:       *interval,
		From:           from,
		To:             to,
	})
	if err != nil {
		log.Fatalf("Yahoo ingestion failed after run %d: %v", result.RunID, err)
	}

	log.Printf(
		"Yahoo ingestion succeeded: run=%d symbol=%s provider_symbol=%s interval=%s received=%d inserted=%d updated=%d rejected=%d",
		result.RunID,
		instrument.CanonicalSymbol,
		source.ProviderSymbol,
		*interval,
		result.RowsReceived,
		result.RowsInserted,
		result.RowsUpdated,
		result.RowsRejected,
	)
}

// parseRequestedRange converts required RFC3339 flags into typed UTC timestamps.
// The end remains exclusive, which prevents a candle at the boundary from being
// loaded into two adjacent ingestion windows.
func parseRequestedRange(fromText, toText string) (pgtype.Timestamptz, pgtype.Timestamptz, error) {
	if strings.TrimSpace(fromText) == "" || strings.TrimSpace(toText) == "" {
		return pgtype.Timestamptz{}, pgtype.Timestamptz{}, fmt.Errorf("--from and --to are required")
	}

	fromTime, err := time.Parse(time.RFC3339, fromText)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.Timestamptz{}, fmt.Errorf("--from must be RFC3339: %w", err)
	}
	toTime, err := time.Parse(time.RFC3339, toText)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.Timestamptz{}, fmt.Errorf("--to must be RFC3339: %w", err)
	}
	if !fromTime.Before(toTime) {
		return pgtype.Timestamptz{}, pgtype.Timestamptz{}, fmt.Errorf("--to must be after --from")
	}

	return pgtype.Timestamptz{Time: fromTime.UTC(), Valid: true}, pgtype.Timestamptz{Time: toTime.UTC(), Valid: true}, nil
}

// findActiveYahooSource selects the active Yahoo mapping for one canonical
// instrument. The mapping keeps provider-specific naming out of user commands.
func findActiveYahooSource(
	ctx context.Context,
	repository *database.InstrumentSourceRepository,
	canonicalSymbol string,
) (database.InstrumentSource, error) {
	sources, err := repository.ListByCanonicalSymbol(ctx, canonicalSymbol)
	if err != nil {
		return database.InstrumentSource{}, err
	}

	for _, source := range sources {
		if source.Provider == "yahoo" && source.IsActive {
			return source, nil
		}
	}

	return database.InstrumentSource{}, fmt.Errorf("no active Yahoo source mapping exists")
}
