package database_test

import (
	// context gives the integration test one deadline for all database operations.
	"context"
	// fmt creates a unique canonical symbol for each test run.
	"fmt"
	// os lets the test remain opt-in instead of changing the database during every unit test run.
	"os"
	// testing provides test discovery, failure reporting, cleanup, and skip behavior.
	"testing"
	// time supplies both a unique test identifier and a PostgreSQL timestamp.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestInstrumentAndSourceRepositoryRoundTrip verifies a complete write/read path
// against the real PostgreSQL database rather than a mock.
//
// Phase 3.3 update: this integration test was added to verify that the connection
// pool, sqlc-generated queries, repository wrappers, foreign keys, and mapping
// queries work together. It is opt-in because it writes temporary rows.
func TestInstrumentAndSourceRepositoryRoundTrip(t *testing.T) {
	// Keep normal go test ./... runs safe and fast. The explicit environment flag
	// tells the developer that this test intentionally requires PostgreSQL.
	if os.Getenv("MONEYPLANT_RUN_INTEGRATION") != "1" {
		t.Skip("set MONEYPLANT_RUN_INTEGRATION=1 to run PostgreSQL integration tests")
	}

	// Load the same environment-based configuration used by the real API.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load test configuration: %v", err)
	}

	// Use one context deadline for pool creation, repository writes, reads, and cleanup.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// NewPool verifies that PostgreSQL is reachable before the repository test begins.
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("create test database pool: %v", err)
	}

	// Register pool shutdown before row-cleanup callbacks. t.Cleanup callbacks
	// execute in reverse registration order, so later row cleanup runs first and
	// this pool closes only after all deletion queries have completed.
	t.Cleanup(func() {
		pool.Close()
	})

	// Construct the same repository objects used by future ingestion and API code.
	instrumentRepository := database.NewInstrumentRepository(pool)
	sourceRepository := database.NewInstrumentSourceRepository(pool)
	marketCandleRepository := database.NewMarketCandleRepository(pool)
	macroDatasetRepository := database.NewMacroDatasetRepository(pool)
	macroObservationRepository := database.NewMacroObservationRepository(pool)

	// Use a unique uppercase symbol because instruments.canonical_symbol is unique
	// and the database check requires canonical symbols to be uppercase.
	testID := time.Now().UnixNano()
	symbol := fmt.Sprintf("TEST%v", testID)
	providerSymbol := fmt.Sprintf("TEST%v.NS", testID)
	exchange := "NSE"
	metadata := []byte(`{"test":true}`)
	var createdSourceID int64

	// Create verifies the INSERT, generated ID, database defaults, and constraints.
	createdInstrument, err := instrumentRepository.Create(
		ctx,
		symbol,
		"MoneyPlant Integration Test Instrument",
		"equity",
		&exchange,
		"INR",
		metadata,
	)
	if err != nil {
		t.Fatalf("create test instrument: %v", err)
	}

	// Always remove temporary rows after the test. The source row must be deleted
	// first because its foreign key references the instrument row.
	t.Cleanup(func() {
		// The main test context is canceled when the test function returns. Cleanup
		// runs after that point, so it needs its own independent deadline.
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()

		// Candles reference the provider source, so delete them before deleting
		// the temporary source and canonical instrument.
		_, cleanupErr := pool.Exec(cleanupContext, "DELETE FROM market_candles WHERE instrument_source_id = $1", createdSourceID)
		if cleanupErr != nil {
			t.Errorf("clean up test market candles: %v", cleanupErr)
		}
		_, cleanupErr = pool.Exec(cleanupContext, "DELETE FROM instrument_sources WHERE instrument_id = $1", createdInstrument.ID)
		if cleanupErr != nil {
			t.Errorf("clean up test source: %v", cleanupErr)
		}
		_, cleanupErr = pool.Exec(cleanupContext, "DELETE FROM instruments WHERE id = $1", createdInstrument.ID)
		if cleanupErr != nil {
			t.Errorf("clean up test instrument: %v", cleanupErr)
		}
	})

	// Read the row back through the repository to verify the generated SELECT path.
	fetchedInstrument, err := instrumentRepository.GetByCanonicalSymbol(ctx, symbol)
	if err != nil {
		t.Fatalf("fetch test instrument: %v", err)
	}
	if fetchedInstrument.ID != createdInstrument.ID {
		t.Fatalf("fetched instrument ID = %d, want %d", fetchedInstrument.ID, createdInstrument.ID)
	}
	if fetchedInstrument.CanonicalSymbol != symbol {
		t.Fatalf("fetched symbol = %q, want %q", fetchedInstrument.CanonicalSymbol, symbol)
	}

	// Create a provider mapping for the same instrument and mark it authoritative.
	createdSource, err := sourceRepository.Create(
		ctx,
		createdInstrument.ID,
		"yahoo",
		providerSymbol,
		nil,
		true,
		[]byte(`{"role":"integration-test"}`),
	)
	if err != nil {
		t.Fatalf("create test provider source: %v", err)
	}
	createdSourceID = createdSource.ID

	// Listing by canonical symbol verifies the join from instruments to mappings.
	sources, err := sourceRepository.ListByCanonicalSymbol(ctx, symbol)
	if err != nil {
		t.Fatalf("list test provider sources: %v", err)
	}
	if len(sources) != 1 || sources[0].ID != createdSource.ID {
		t.Fatalf("listed sources = %#v, want one source with ID %d", sources, createdSource.ID)
	}

	// The authoritative lookup verifies the partial unique-index policy and the
	// query path used later to select a provider for ingestion.
	authoritativeSource, err := sourceRepository.GetAuthoritativeByCanonicalSymbol(ctx, symbol)
	if err != nil {
		t.Fatalf("get authoritative test source: %v", err)
	}
	if authoritativeSource.Provider != "yahoo" {
		t.Fatalf("authoritative provider = %q, want %q", authoritativeSource.Provider, "yahoo")
	}

	// Phase 5 integration verification: reuse one valid retrieval timestamp for
	// the temporary market candle and ingestion-run records created by this test.
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}

	// Phase 5.4 update: verify the real market repository's insert/update paths
	// against PostgreSQL, including the natural-key uniqueness behavior.
	marketObservedAt := pgtype.Timestamptz{Time: time.Date(2026, time.January, 5, 9, 15, 0, 0, time.UTC), Valid: true}
	marketInput := database.MarketCandleInput{
		InstrumentSourceID: createdSource.ID,
		Interval:           "1d",
		ObservedAt:         marketObservedAt,
		SourceCloseAt:      pgtype.Timestamptz{Time: time.Date(2026, time.January, 6, 9, 14, 59, 999000000, time.UTC), Valid: true},
		Open:               integrationNumeric("100"),
		High:               integrationNumeric("105"),
		Low:                integrationNumeric("95"),
		Close:              integrationNumeric("102"),
		Volume:             integrationNumeric("1000"),
		SourceRetrievedAt:  now,
	}
	firstMarketWrite, err := marketCandleRepository.Upsert(ctx, marketInput)
	if err != nil {
		t.Fatalf("insert test market candle: %v", err)
	}
	if !firstMarketWrite.Inserted {
		t.Fatal("first market upsert reported update, want insert")
	}

	marketInput.Close = integrationNumeric("103")
	secondMarketWrite, err := marketCandleRepository.Upsert(ctx, marketInput)
	if err != nil {
		t.Fatalf("update test market candle: %v", err)
	}
	if secondMarketWrite.Inserted {
		t.Fatal("second market upsert reported insert, want update")
	}
	marketRows, err := marketCandleRepository.ListByCanonicalSymbol(ctx, symbol, "yahoo", "1d", marketObservedAt, pgtype.Timestamptz{Time: marketObservedAt.Time.Add(24 * time.Hour), Valid: true})
	if err != nil {
		t.Fatalf("list test market candles: %v", err)
	}
	if len(marketRows) != 1 {
		t.Fatalf("market rows = %d, want one row after repeated upsert", len(marketRows))
	}

	// Phase 5.4 update: perform the same insert/update verification for macro
	// observations, whose natural key is dataset plus observed_on date.
	testMacroCode := fmt.Sprintf("integration_macro_%v", testID)
	createdDataset, err := macroDatasetRepository.Create(
		ctx,
		testMacroCode,
		"MoneyPlant Integration Test Macro Dataset",
		"test",
		"Integration metric",
		"percent",
		"monthly",
		"reference_period",
		nil,
		"https://example.invalid/moneyplant-integration",
		now,
		[]byte(`{"test":true}`),
	)
	if err != nil {
		t.Fatalf("create test macro dataset: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, cleanupErr := pool.Exec(cleanupContext, "DELETE FROM macro_observations WHERE macro_dataset_id = $1", createdDataset.ID); cleanupErr != nil {
			t.Errorf("clean up test macro observations: %v", cleanupErr)
		}
		if _, cleanupErr := pool.Exec(cleanupContext, "DELETE FROM macro_datasets WHERE id = $1", createdDataset.ID); cleanupErr != nil {
			t.Errorf("clean up test macro dataset: %v", cleanupErr)
		}
	})

	macroDate := pgtype.Date{Time: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	firstMacroWrite, err := macroObservationRepository.Upsert(ctx, createdDataset.ID, macroDate, integrationNumeric("2.73"), now, nil, []byte(`{"test":true}`))
	if err != nil {
		t.Fatalf("insert test macro observation: %v", err)
	}
	if !firstMacroWrite.Inserted {
		t.Fatal("first macro upsert reported update, want insert")
	}
	secondMacroWrite, err := macroObservationRepository.Upsert(ctx, createdDataset.ID, macroDate, integrationNumeric("2.74"), now, nil, []byte(`{"test":true,"updated":true}`))
	if err != nil {
		t.Fatalf("update test macro observation: %v", err)
	}
	if secondMacroWrite.Inserted {
		t.Fatal("second macro upsert reported insert, want update")
	}
	macroRows, err := macroObservationRepository.ListByDatasetCode(ctx, testMacroCode)
	if err != nil {
		t.Fatalf("list test macro observations: %v", err)
	}
	if len(macroRows) != 1 {
		t.Fatalf("macro rows = %d, want one row after repeated upsert", len(macroRows))
	}

	// Exercise the ingestion repository using the same run lifecycle as a batch job.
	ingestionRepository := database.NewIngestionRunRepository(pool)
	runScope := []byte(fmt.Sprintf(`{"symbol":%q}`, providerSymbol))
	run, err := ingestionRepository.Create(ctx, "market_api", "yahoo", now, pgtype.Timestamptz{}, pgtype.Timestamptz{}, runScope)
	if err != nil {
		t.Fatalf("create test ingestion run: %v", err)
	}
	if run.Status != "running" {
		t.Fatalf("new ingestion run status = %q, want %q", run.Status, "running")
	}

	// Ingestion runs do not have a foreign key to the temporary instrument, so
	// register their cleanup separately after the run ID becomes available.
	t.Cleanup(func() {
		// Use a fresh context because the main test context is already canceled
		// when deferred cleanup functions execute.
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()

		if _, cleanupErr := pool.Exec(cleanupContext, "DELETE FROM ingestion_runs WHERE id = $1", run.ID); cleanupErr != nil {
			t.Errorf("clean up test ingestion run: %v", cleanupErr)
		}
	})

	// Complete the run to verify lifecycle updates and row-count persistence.
	completed, err := ingestionRepository.Complete(ctx, run.ID, "succeeded", now, 1, 1, 0, 0, nil)
	if err != nil {
		t.Fatalf("complete test ingestion run: %v", err)
	}
	if completed.Status != "succeeded" || completed.RowsInserted != 1 {
		t.Fatalf("completed ingestion run = %#v, want succeeded with one insert", completed)
	}
}

// integrationNumeric converts an exact decimal string into the pgtype numeric
// representation used by the repositories, avoiding float64 in the test path.
func integrationNumeric(value string) pgtype.Numeric {
	var numericValue pgtype.Numeric
	if err := numericValue.Scan(value); err != nil {
		panic(err)
	}
	return numericValue
}
