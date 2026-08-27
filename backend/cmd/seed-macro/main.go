// Command seed-macro loads one reviewed macro CSV file into PostgreSQL.
package main

import (
	// context carries cancellation through file reading, database writes, and audit updates.
	"context"
	// flag provides the dataset and file options without adding a CLI dependency.
	"flag"
	// log prints progress and exits with a failure status for unrecoverable errors.
	"log"
	// os opens the selected CSV file and ensures it is closed after seeding.
	"os"
	// strings normalizes the stable dataset code before lookup.
	"strings"
	// time records when the local source file was read.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// main coordinates one macro CSV seed operation.
//
// Phase 4.5 update: this command was added as the macro composition root. It
// connects a local CSV file to the shared reader, PostgreSQL repositories, and
// macro ingestion service without placing file or database logic in the reader.
func main() {
	// One command processes one dataset so the audit row has one unambiguous
	// dataset code and source file. Run the command once for CPI and once for the
	// RBI repo-rate dataset.
	datasetCode := flag.String("dataset", "", "MoneyPlant macro dataset code")
	filePath := flag.String("file", "", "path to a reviewed macro CSV file")
	flag.Parse()

	if strings.TrimSpace(*datasetCode) == "" || strings.TrimSpace(*filePath) == "" {
		log.Fatal("--dataset and --file are required")
	}

	// Open the source before creating database infrastructure. If the path is
	// wrong, the command stops immediately without creating an ingestion run.
	sourceFile, err := os.Open(*filePath)
	if err != nil {
		log.Fatalf("open macro CSV %q: %v", *filePath, err)
	}
	defer sourceFile.Close()

	// The reader records the retrieval time of this local source file. This is
	// provenance metadata and is intentionally different from observed_on, which
	// identifies the month or event represented by each row.
	reader, err := ingestion.NewCSVMacroReader(strings.TrimSpace(*datasetCode), time.Now().UTC())
	if err != nil {
		log.Fatalf("create macro CSV reader: %v", err)
	}

	// Load and validate environment configuration before opening PostgreSQL.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	// The bounded context keeps a local seed command from hanging indefinitely,
	// while still allowing a reviewed CSV with many rows to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// NewPool creates one reusable PostgreSQL pool and verifies it with Ping.
	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		log.Fatalf("database startup error: %v", err)
	}
	defer pool.Close()

	// Construct the service from repositories and the reader. The service resolves
	// the dataset definition, creates a macro_seed audit row, upserts observations,
	// and records final counts.
	service := ingestion.NewMacroIngestionService(
		reader,
		database.NewMacroDatasetRepository(pool),
		database.NewMacroObservationRepository(pool),
		database.NewIngestionRunRepository(pool),
	)

	result, err := service.Seed(ctx, sourceFile)
	if err != nil {
		log.Fatalf("macro seed failed after run %d: %v", result.RunID, err)
	}

	log.Printf(
		"macro seed succeeded: run=%d dataset=%s received=%d inserted=%d updated=%d rejected=%d",
		result.RunID,
		reader.DatasetCode(),
		result.RowsReceived,
		result.RowsInserted,
		result.RowsUpdated,
		result.RowsRejected,
	)
}
