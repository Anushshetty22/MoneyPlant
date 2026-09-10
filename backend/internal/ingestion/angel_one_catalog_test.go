package ingestion_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
	"github.com/jackc/pgx/v5"
)

func TestResolveAngelOneInstrumentMappingsResolvesInitialSix(t *testing.T) {
	records := []ingestion.AngelOneInstrumentMasterRecord{
		{Token: "fixture-nifty", TradingSymbol: "NIFTY", Name: "NIFTY", ExchangeSegment: "nse_cm", InstrumentType: "AMXIDX"},
		{Token: "fixture-sbin", TradingSymbol: "SBIN-EQ", Name: "SBIN", ExchangeSegment: "NSE"},
		{Token: "fixture-reliance", TradingSymbol: "RELIANCE-EQ", Name: "RELIANCE", ExchangeSegment: "nse_cm"},
		{Token: "fixture-tcs", TradingSymbol: "TCS-EQ", Name: "TCS", ExchangeSegment: "NSE"},
		{Token: "fixture-infy", TradingSymbol: "INFY-EQ", Name: "INFY", ExchangeSegment: "NSE"},
		{Token: "fixture-hdfc", TradingSymbol: "HDFCBANK-EQ", Name: "HDFCBANK", ExchangeSegment: "NSE"},
	}

	mappings, err := ingestion.ResolveAngelOneInstrumentMappings(records, ingestion.InitialAngelOneInstrumentDefinitions())
	if err != nil {
		t.Fatalf("resolve catalog: %v", err)
	}
	if len(mappings) != 6 {
		t.Fatalf("mapping count = %d, want 6", len(mappings))
	}
	if mappings[0].Definition.CanonicalSymbol != "NIFTY50" || mappings[0].ProviderSymbol != "NIFTY" {
		t.Fatalf("NIFTY mapping = %#v", mappings[0])
	}
	if mappings[0].Exchange != "NSE" || mappings[0].ProviderInstrumentID != "fixture-nifty" {
		t.Fatalf("NIFTY identity = %#v", mappings[0])
	}
}

func TestAngelOneInstrumentMasterFixtureResolvesInitialSix(t *testing.T) {
	fixture, err := os.Open("../../testdata/angel_one_instrument_master.json")
	if err != nil {
		t.Fatalf("open instrument-master fixture: %v", err)
	}
	defer fixture.Close()

	records, err := ingestion.ParseAngelOneInstrumentMaster(fixture)
	if err != nil {
		t.Fatalf("parse instrument-master fixture: %v", err)
	}
	mappings, err := ingestion.ResolveAngelOneInstrumentMappings(records, ingestion.InitialAngelOneInstrumentDefinitions())
	if err != nil {
		t.Fatalf("resolve instrument-master fixture: %v", err)
	}
	if len(mappings) != 6 {
		t.Fatalf("fixture mapping count = %d, want 6", len(mappings))
	}
}

func TestParseAngelOneInstrumentMasterRejectsMalformedRowsAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{name: "missing token", json: `[{"symbol":"SBIN-EQ","exch_seg":"NSE"}]`, want: "no token"},
		{name: "trailing value", json: `[{"token":"1","symbol":"SBIN-EQ","exch_seg":"NSE"}] {}`, want: "more than one JSON value"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ingestion.ParseAngelOneInstrumentMaster(strings.NewReader(test.json))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestResolveAngelOneInstrumentMappingsRejectsMissingAndDuplicateMappings(t *testing.T) {
	definitions := ingestion.InitialAngelOneInstrumentDefinitions()
	base := ingestion.AngelOneInstrumentMasterRecord{Token: "1", TradingSymbol: "SBIN-EQ", ExchangeSegment: "NSE"}

	missing := append([]ingestion.AngelOneInstrumentMasterRecord(nil), base)
	_, err := ingestion.ResolveAngelOneInstrumentMappings(missing, definitions)
	if err == nil || !strings.Contains(err.Error(), "mapping missing for NIFTY50") {
		t.Fatalf("missing error = %v", err)
	}

	duplicate := append([]ingestion.AngelOneInstrumentMasterRecord(nil), base, base)
	_, err = ingestion.ResolveAngelOneInstrumentMappings(duplicate, []ingestion.AngelOneCanonicalInstrumentDefinition{definitions[1]})
	if err == nil || !strings.Contains(err.Error(), "duplicate Angel One mappings for SBIN") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestPersistAngelOneCatalogCreatesInstrumentsAndRefreshesMappings(t *testing.T) {
	definitions := ingestion.InitialAngelOneInstrumentDefinitions()
	mappings := []ingestion.AngelOneResolvedMapping{
		{
			Definition:           definitions[1],
			Exchange:             "NSE",
			ProviderSymbol:       "SBIN-EQ",
			ProviderInstrumentID: "3045",
			Metadata:             []byte(`{"exchange_segment":"nse_cm"}`),
		},
	}
	instruments := &fakeCatalogInstrumentRepository{getError: pgx.ErrNoRows}
	sources := &fakeCatalogSourceRepository{}

	persisted, err := ingestion.PersistAngelOneCatalog(context.Background(), mappings, instruments, sources)
	if err != nil {
		t.Fatalf("persist catalog: %v", err)
	}
	if len(persisted) != 1 || persisted[0].Provider != string(ingestion.ProviderAngelOne) {
		t.Fatalf("persisted sources = %#v", persisted)
	}
	if instruments.created.CanonicalSymbol != "SBIN" || sources.upserted.ProviderInstrumentID == nil || *sources.upserted.ProviderInstrumentID != "3045" {
		t.Fatalf("persisted instrument/source = %#v / %#v", instruments.created, sources.upserted)
	}

	if !reflect.DeepEqual(sources.upserted.Metadata, mappings[0].Metadata) {
		t.Fatalf("metadata = %s, want %s", sources.upserted.Metadata, mappings[0].Metadata)
	}
}

type fakeCatalogInstrumentRepository struct {
	created  database.Instrument
	getError error
}

func (f *fakeCatalogInstrumentRepository) Create(_ context.Context, canonicalSymbol, name, assetType string, exchange *string, currency string, metadata []byte) (database.Instrument, error) {
	f.created = database.Instrument{ID: 42, CanonicalSymbol: canonicalSymbol, Name: name, AssetType: assetType, Exchange: exchange, Currency: currency, Metadata: metadata, IsActive: true}
	return f.created, nil
}

func (f *fakeCatalogInstrumentRepository) GetByCanonicalSymbol(context.Context, string) (database.Instrument, error) {
	return database.Instrument{}, f.getError
}

type fakeCatalogSourceRepository struct {
	upserted database.InstrumentSource
}

func (f *fakeCatalogSourceRepository) Upsert(_ context.Context, instrumentID int64, provider, providerSymbol string, providerInstrumentID *string, authoritative bool, metadata []byte) (database.InstrumentSource, error) {
	f.upserted = database.InstrumentSource{ID: 7, InstrumentID: instrumentID, Provider: provider, ProviderSymbol: providerSymbol, ProviderInstrumentID: providerInstrumentID, IsAuthoritative: authoritative, IsActive: true, Metadata: metadata}
	return f.upserted, nil
}
