// Command ingest-binance downloads Binance historical klines and stores them in PostgreSQL.
package main

import (
	// context carries cancellation through configuration, database, HTTP, and repository calls.
	"context"
	// flag provides simple command-line options without adding a third-party CLI dependency.
	"flag"
	// fmt creates validation errors for command-line timestamps and source resolution.
	"fmt"
	// log prints progress and stops the command with a non-zero exit code on fatal errors.
	"log"
	// strings normalizes the user-facing canonical symbol and compares provider names safely.
	"strings"
	// time parses the RFC3339 command-line dates and supplies a bounded startup timeout.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

// main coordinates one user-requested Binance batch.
//
// Phase 4.2 update: this command was added as the first real composition root
// for ingestion. It connects configuration, PostgreSQL repositories, the
// Binance adapter, and the provider-independent MarketIngestionService.
func main() {
	// Flags make the requested symbol and time window explicit at execution time.
	// The end timestamp is exclusive, matching both the repository query contract
	// and the Binance adapter's conversion of To into an inclusive endTime minus
	// one millisecond.
	canonicalSymbol := flag.String("symbol", "BTCUSDT", "canonical MoneyPlant symbol, for example BTCUSDT")
	interval := flag.String("interval", "1d", "candle interval, for example 1d or 1h")
	fromText := flag.String("from", "", "UTC start timestamp in RFC3339 format, inclusive")
	toText := flag.String("to", "", "UTC end timestamp in RFC3339 format, exclusive")
	flag.Parse()

	from, to, err := parseRequestedRange(*fromText, *toText)
	if err != nil {
		log.Fatalf("invalid requested range: %v", err)
	}

	// config.Load is the single environment-variable entry point. It applies the
	// local PostgreSQL defaults used by Docker and validates port values before
	// any infrastructure is created.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	// A command context allows Ctrl+C cancellation to reach the database and
	// HTTP layers. The five-minute limit protects a local learning command from
	// hanging forever because of a network or provider problem.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// NewPool creates one reusable PostgreSQL pool and pings the database before
	// the command resolves instruments. Closing the pool releases all connections
	// when the command returns.
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		log.Fatalf("database startup error: %v", err)
	}
	defer pool.Close()

	// Resolve the canonical instrument first. This confirms the user supplied a
	// known MoneyPlant definition and gives the pipeline the stable application
	// symbol used in ingestion-run scope.
	instrumentRepository := database.NewInstrumentRepository(pool)
	instrument, err := instrumentRepository.GetByCanonicalSymbol(ctx, strings.ToUpper(strings.TrimSpace(*canonicalSymbol)))
	if err != nil {
		log.Fatalf("resolve instrument %q: %v", *canonicalSymbol, err)
	}

	// Resolve the provider-specific source mapping separately. The source table
	// tells us the exact Binance symbol; this avoids assuming that every future
	// canonical instrument uses the same provider identifier.
	sourceRepository := database.NewInstrumentSourceRepository(pool)
	source, err := findActiveBinanceSource(ctx, sourceRepository, instrument.CanonicalSymbol)
	if err != nil {
		log.Fatalf("resolve Binance source for %s: %v", instrument.CanonicalSymbol, err)
	}

	// The adapter uses the source mapping's provider symbol and the public Binance
	// market-data endpoint. No API key is required for this public klines request.
	provider, err := ingestion.NewBinanceMarketDataProvider(nil, ingestion.BinancePublicDataBaseURL)
	if err != nil {
		log.Fatalf("create Binance provider: %v", err)
	}

	// MarketIngestionService owns the batch lifecycle: it creates an ingestion
	// audit row, fetches normalized provider rows, assigns instrument_source_id,
	// stores candles, and completes the audit row with counts and status.
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
		log.Fatalf("Binance ingestion failed after run %d: %v", result.RunID, err)
	}

	log.Printf(
		"Binance ingestion succeeded: run=%d symbol=%s provider_symbol=%s interval=%s received=%d inserted=%d updated=%d rejected=%d",
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

// parseRequestedRange converts required RFC3339 flags into PostgreSQL timestamp
// values. Keeping parsing at the command boundary means the ingestion service
// receives typed timestamps and does not need to know about CLI text formats.
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

// findActiveBinanceSource selects the active Binance mapping for one canonical
// instrument. It uses the repository abstraction so the command does not know
// the SQL join or generated sqlc row types.
func findActiveBinanceSource(
	ctx context.Context,
	repository *database.InstrumentSourceRepository,
	canonicalSymbol string,
) (database.InstrumentSource, error) {
	sources, err := repository.ListByCanonicalSymbol(ctx, canonicalSymbol)
	if err != nil {
		return database.InstrumentSource{}, err
	}

	for _, source := range sources {
		if source.Provider == "binance" && source.IsActive {
			return source, nil
		}
	}

	return database.InstrumentSource{}, fmt.Errorf("no active Binance source mapping exists")
}
