// Command catalog-angelone resolves a local Angel One instrument-master file
// into MoneyPlant's canonical instruments and provider mappings.
package main

import (
	// context carries cancellation through the file, resolver, and PostgreSQL.
	"context"
	// flag keeps this credential-free maintenance command small and explicit.
	"flag"
	// log reports progress and exits non-zero when catalog persistence fails.
	"log"
	// os opens the operator-provided instrument-master file.
	"os"
	// time bounds a local catalog refresh so a broken database cannot hang it.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

func main() {
	masterPath := flag.String("file", "", "path to an Angel One OpenAPIScripMaster.json file")
	flag.Parse()
	if *masterPath == "" {
		log.Fatal("--file is required")
	}

	masterFile, err := os.Open(*masterPath)
	if err != nil {
		log.Fatalf("open Angel One instrument master: %v", err)
	}
	defer masterFile.Close()

	records, err := ingestion.ParseAngelOneInstrumentMaster(masterFile)
	if err != nil {
		log.Fatalf("parse Angel One instrument master: %v", err)
	}
	mappings, err := ingestion.ResolveAngelOneInstrumentMappings(
		records,
		ingestion.InitialAngelOneInstrumentDefinitions(),
	)
	if err != nil {
		log.Fatalf("resolve Angel One catalog: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		log.Fatalf("database startup error: %v", err)
	}
	defer pool.Close()

	persisted, err := ingestion.PersistAngelOneCatalog(
		ctx,
		mappings,
		database.NewInstrumentRepository(pool),
		database.NewInstrumentSourceRepository(pool),
	)
	if err != nil {
		log.Fatalf("persist Angel One catalog: %v", err)
	}

	log.Printf("Angel One catalog refreshed: mappings=%d", len(persisted))
	for _, source := range persisted {
		log.Printf(
			"resolved canonical instrument source: instrument_id=%d provider_symbol=%s token=%s",
			source.InstrumentID,
			source.ProviderSymbol,
			valueOrEmpty(source.ProviderInstrumentID),
		)
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
