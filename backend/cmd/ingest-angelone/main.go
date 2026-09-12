// Command ingest-angelone downloads authenticated Angel One historical candles
// and stores them through the provider-independent MoneyPlant pipeline.
package main

import (
	// context carries cancellation through authentication, database, and HTTP.
	"context"
	// flag keeps this learning command explicit without adding a CLI framework.
	"flag"
	// fmt creates command-line range validation errors.
	"fmt"
	// log reports progress without printing credentials or provider tokens.
	"log"
	// strings normalizes the canonical symbol and provider comparison.
	"strings"
	// time parses RFC3339 flags and bounds the complete command.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5/pgtype"
)

func main() {
	canonicalSymbol := flag.String("symbol", "NIFTY50", "canonical MoneyPlant symbol, for example NIFTY50 or SBIN")
	interval := flag.String("interval", "1d", "Angel One interval: 1m, 5m, 15m, 30m, 1h, or 1d")
	fromText := flag.String("from", "", "UTC start timestamp in RFC3339 format, inclusive")
	toText := flag.String("to", "", "UTC end timestamp in RFC3339 format, exclusive")
	flag.Parse()

	from, to, err := parseAngelOneRequestedRange(*fromText, *toText)
	if err != nil {
		log.Fatalf("invalid requested range: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	authenticator, err := ingestion.NewAngelOneAuthenticatorWithSettings(
		nil,
		ingestion.AngelOneAuthSettings{
			BaseURL:        cfg.AngelOneBaseURL,
			ClientLocalIP:  cfg.AngelOneClientLocalIP,
			ClientPublicIP: cfg.AngelOneClientPublicIP,
			MACAddress:     cfg.AngelOneMACAddress,
		},
		ingestion.AngelOneCredentials{
			APIKey:     cfg.AngelOneAPIKey,
			ClientCode: cfg.AngelOneClientCode,
			Password:   cfg.AngelOnePassword,
			TOTPSecret: cfg.AngelOneTOTPSecret,
		},
	)
	if err != nil {
		log.Fatalf("Angel One configuration error: %v", err)
	}
	session, err := authenticator.Login(ctx)
	if err != nil {
		log.Fatalf("Angel One authentication failed: %v", err)
	}

	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		log.Fatalf("database startup error: %v", err)
	}
	defer pool.Close()

	instrumentRepository := database.NewInstrumentRepository(pool)
	instrument, err := instrumentRepository.GetByCanonicalSymbol(ctx, strings.ToUpper(strings.TrimSpace(*canonicalSymbol)))
	if err != nil {
		log.Fatalf("resolve instrument %q: %v", *canonicalSymbol, err)
	}
	sourceRepository := database.NewInstrumentSourceRepository(pool)
	source, err := findActiveAngelOneSource(ctx, sourceRepository, instrument.CanonicalSymbol)
	if err != nil {
		log.Fatalf("resolve Angel One source for %s: %v", instrument.CanonicalSymbol, err)
	}
	if source.ProviderInstrumentID == nil || strings.TrimSpace(*source.ProviderInstrumentID) == "" {
		log.Fatalf("Angel One source for %s has no instrument token; refresh the catalog first", instrument.CanonicalSymbol)
	}

	provider, err := ingestion.NewAngelOneHistoricalMarketDataProvider(nil, authenticator, session)
	if err != nil {
		log.Fatalf("create Angel One historical provider: %v", err)
	}
	service := ingestion.NewMarketIngestionService(
		provider,
		database.NewMarketCandleRepository(pool),
		database.NewIngestionRunRepository(pool),
	)

	result, err := service.IngestHistorical(ctx, instrument.CanonicalSymbol, source.ID, ingestion.HistoricalCandleRequest{
		CanonicalSymbol:      instrument.CanonicalSymbol,
		ProviderSymbol:       source.ProviderSymbol,
		ProviderInstrumentID: *source.ProviderInstrumentID,
		Interval:             *interval,
		From:                 from,
		To:                   to,
	})
	if err != nil {
		log.Fatalf("Angel One ingestion failed after run %d: %v", result.RunID, err)
	}

	log.Printf(
		"Angel One ingestion succeeded: run=%d symbol=%s provider_symbol=%s interval=%s received=%d inserted=%d updated=%d rejected=%d",
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

func parseAngelOneRequestedRange(fromText, toText string) (pgtype.Timestamptz, pgtype.Timestamptz, error) {
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

func findActiveAngelOneSource(
	ctx context.Context,
	repository *database.InstrumentSourceRepository,
	canonicalSymbol string,
) (database.InstrumentSource, error) {
	sources, err := repository.ListByCanonicalSymbol(ctx, canonicalSymbol)
	if err != nil {
		return database.InstrumentSource{}, err
	}
	for _, source := range sources {
		if source.Provider == string(ingestion.ProviderAngelOne) && source.IsActive {
			return source, nil
		}
	}
	return database.InstrumentSource{}, fmt.Errorf("no active Angel One source mapping exists")
}
