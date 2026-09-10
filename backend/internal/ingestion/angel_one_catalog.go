package ingestion

import (
	// context carries cancellation through database-backed catalog persistence.
	"context"
	// encoding/json decodes Angel One's instrument-master document and stores
	// small provider details as metadata without inventing database columns.
	"encoding/json"
	// errors lets persistence distinguish a missing canonical instrument from a
	// database failure that should be returned to the operator.
	"errors"
	// fmt adds the canonical symbol and provider details to readable failures.
	"fmt"
	// io.Reader keeps the parser usable with a fixture file, an HTTP response, or
	// an in-memory test string without coupling it to network code.
	"io"
	// strings normalizes exchange segments and compares provider symbols safely.
	"strings"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5"
)

// AngelOneInstrumentMasterRecord is the subset of one Angel One master row
// needed to resolve a MoneyPlant instrument. The provider publishes these
// values as strings, including numeric-looking tokens and tick sizes.
type AngelOneInstrumentMasterRecord struct {
	Token           string `json:"token"`
	TradingSymbol   string `json:"symbol"`
	Name            string `json:"name"`
	ExchangeSegment string `json:"exch_seg"`
	InstrumentType  string `json:"instrumenttype"`
	Expiry          string `json:"expiry"`
	Strike          string `json:"strike"`
	LotSize         string `json:"lotsize"`
	TickSize        string `json:"tick_size"`
}

// AngelOneCanonicalInstrumentDefinition describes the six initial MoneyPlant
// identities and the provider symbols that may represent each one. The token
// is intentionally absent: it must always come from the current master file.
type AngelOneCanonicalInstrumentDefinition struct {
	CanonicalSymbol string
	Name            string
	AssetType       string
	Exchange        string
	Currency        string
	ProviderSymbols []string
}

// AngelOneResolvedMapping is the safe output of catalog resolution. It has
// both the stable MoneyPlant identity and the exact provider symbol/token that
// later historical and live adapters will send to Angel One.
type AngelOneResolvedMapping struct {
	Definition           AngelOneCanonicalInstrumentDefinition
	Exchange             string
	ProviderSymbol       string
	ProviderInstrumentID string
	Metadata             []byte
}

// angelOneInstrumentCatalogRepository is intentionally narrower than the
// general ingestion repository contract. Catalog refresh only needs lookup and
// creation, which keeps its tests independent from unrelated ingestion methods.
type angelOneInstrumentCatalogRepository interface {
	Create(context.Context, string, string, string, *string, string, []byte) (database.Instrument, error)
	GetByCanonicalSymbol(context.Context, string) (database.Instrument, error)
}

// angelOneSourceCatalogRepository contains only the refresh operation needed
// to store provider mappings from the latest instrument master.
type angelOneSourceCatalogRepository interface {
	Upsert(context.Context, int64, string, string, *string, bool, []byte) (database.InstrumentSource, error)
}

// InitialAngelOneInstrumentDefinitions returns the six project-approved
// canonical instruments. A fresh slice prevents callers from mutating the
// package-level catalog policy accidentally.
func InitialAngelOneInstrumentDefinitions() []AngelOneCanonicalInstrumentDefinition {
	return []AngelOneCanonicalInstrumentDefinition{
		{
			CanonicalSymbol: "NIFTY50",
			Name:            "NIFTY 50",
			AssetType:       "index",
			Exchange:        "NSE",
			Currency:        "INR",
			ProviderSymbols: []string{"NIFTY", "NIFTY50", "NIFTY 50"},
		},
		{
			CanonicalSymbol: "SBIN",
			Name:            "State Bank of India",
			AssetType:       "equity",
			Exchange:        "NSE",
			Currency:        "INR",
			ProviderSymbols: []string{"SBIN-EQ"},
		},
		{
			CanonicalSymbol: "RELIANCE",
			Name:            "Reliance Industries",
			AssetType:       "equity",
			Exchange:        "NSE",
			Currency:        "INR",
			ProviderSymbols: []string{"RELIANCE-EQ"},
		},
		{
			CanonicalSymbol: "TCS",
			Name:            "Tata Consultancy Services",
			AssetType:       "equity",
			Exchange:        "NSE",
			Currency:        "INR",
			ProviderSymbols: []string{"TCS-EQ"},
		},
		{
			CanonicalSymbol: "INFY",
			Name:            "Infosys",
			AssetType:       "equity",
			Exchange:        "NSE",
			Currency:        "INR",
			ProviderSymbols: []string{"INFY-EQ"},
		},
		{
			CanonicalSymbol: "HDFCBANK",
			Name:            "HDFC Bank",
			AssetType:       "equity",
			Exchange:        "NSE",
			Currency:        "INR",
			ProviderSymbols: []string{"HDFCBANK-EQ"},
		},
	}
}

// ParseAngelOneInstrumentMaster decodes one complete Angel One instrument
// master JSON array and validates the identity fields required for resolution.
// It does not contact Angel One and does not persist anything.
func ParseAngelOneInstrumentMaster(reader io.Reader) ([]AngelOneInstrumentMasterRecord, error) {
	if reader == nil {
		return nil, fmt.Errorf("Angel One instrument master reader cannot be nil")
	}

	decoder := json.NewDecoder(reader)
	var records []AngelOneInstrumentMasterRecord
	if err := decoder.Decode(&records); err != nil {
		return nil, fmt.Errorf("decode Angel One instrument master: %w", err)
	}
	if records == nil {
		return nil, fmt.Errorf("Angel One instrument master must be a JSON array")
	}

	for index, record := range records {
		if strings.TrimSpace(record.Token) == "" {
			return nil, fmt.Errorf("instrument master row %d has no token", index)
		}
		if strings.TrimSpace(record.TradingSymbol) == "" {
			return nil, fmt.Errorf("instrument master row %d has no trading symbol", index)
		}
		if strings.TrimSpace(record.ExchangeSegment) == "" {
			return nil, fmt.Errorf("instrument master row %d has no exchange segment", index)
		}
	}

	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("Angel One instrument master contains more than one JSON value")
		}
		return nil, fmt.Errorf("read Angel One instrument master tail: %w", err)
	}

	return records, nil
}

// ResolveAngelOneInstrumentMappings matches the configured canonical
// definitions against the current provider master. Matching uses exchange and
// trading symbol together, because a symbol can exist on more than one venue.
func ResolveAngelOneInstrumentMappings(
	records []AngelOneInstrumentMasterRecord,
	definitions []AngelOneCanonicalInstrumentDefinition,
) ([]AngelOneResolvedMapping, error) {
	if len(definitions) == 0 {
		return nil, fmt.Errorf("Angel One catalog definitions cannot be empty")
	}

	seenDefinitions := make(map[string]struct{}, len(definitions))
	resolved := make([]AngelOneResolvedMapping, 0, len(definitions))
	for _, definition := range definitions {
		canonical := strings.TrimSpace(strings.ToUpper(definition.CanonicalSymbol))
		if canonical == "" {
			return nil, fmt.Errorf("Angel One catalog definition has an empty canonical symbol")
		}
		if _, exists := seenDefinitions[canonical]; exists {
			return nil, fmt.Errorf("duplicate Angel One catalog definition for %s", canonical)
		}
		seenDefinitions[canonical] = struct{}{}

		matches := make([]AngelOneInstrumentMasterRecord, 0, 1)
		for _, record := range records {
			if normalizeExchange(record.ExchangeSegment) != normalizeExchange(definition.Exchange) {
				continue
			}
			if !containsProviderSymbol(definition.ProviderSymbols, record.TradingSymbol) {
				continue
			}
			matches = append(matches, record)
		}

		if len(matches) == 0 {
			return nil, fmt.Errorf(
				"Angel One mapping missing for %s: exchange=%s symbols=%v",
				canonical,
				definition.Exchange,
				definition.ProviderSymbols,
			)
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf(
				"duplicate Angel One mappings for %s: found %d rows for exchange=%s symbols=%v",
				canonical,
				len(matches),
				definition.Exchange,
				definition.ProviderSymbols,
			)
		}

		match := matches[0]
		metadata, err := json.Marshal(map[string]string{
			"exchange_segment": strings.TrimSpace(match.ExchangeSegment),
			"instrument_type":  strings.TrimSpace(match.InstrumentType),
			"name":             strings.TrimSpace(match.Name),
			"expiry":           strings.TrimSpace(match.Expiry),
			"strike":           strings.TrimSpace(match.Strike),
			"lotsize":          strings.TrimSpace(match.LotSize),
			"tick_size":        strings.TrimSpace(match.TickSize),
		})
		if err != nil {
			return nil, fmt.Errorf("build metadata for %s: %w", canonical, err)
		}

		definition.CanonicalSymbol = canonical
		resolved = append(resolved, AngelOneResolvedMapping{
			Definition:           definition,
			Exchange:             normalizeExchange(match.ExchangeSegment),
			ProviderSymbol:       strings.TrimSpace(match.TradingSymbol),
			ProviderInstrumentID: strings.TrimSpace(match.Token),
			Metadata:             metadata,
		})
	}

	return resolved, nil
}

// PersistAngelOneCatalog creates missing canonical instruments and upserts the
// resolved Angel One mappings. Existing tokens are refreshed from the current
// master file, so no token is hardcoded in code or SQL.
func PersistAngelOneCatalog(
	ctx context.Context,
	mappings []AngelOneResolvedMapping,
	instrumentRepository angelOneInstrumentCatalogRepository,
	sourceRepository angelOneSourceCatalogRepository,
) ([]database.InstrumentSource, error) {
	if instrumentRepository == nil || sourceRepository == nil {
		return nil, fmt.Errorf("Angel One catalog repositories cannot be nil")
	}

	persisted := make([]database.InstrumentSource, 0, len(mappings))
	for _, mapping := range mappings {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		definition := mapping.Definition
		instrument, err := instrumentRepository.GetByCanonicalSymbol(ctx, definition.CanonicalSymbol)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("resolve canonical instrument %s: %w", definition.CanonicalSymbol, err)
			}

			exchange := definition.Exchange
			instrument, err = instrumentRepository.Create(
				ctx,
				definition.CanonicalSymbol,
				definition.Name,
				definition.AssetType,
				&exchange,
				definition.Currency,
				[]byte(`{"catalog":"angel_one"}`),
			)
			if err != nil {
				return nil, fmt.Errorf("create canonical instrument %s: %w", definition.CanonicalSymbol, err)
			}
		}

		providerID := mapping.ProviderInstrumentID
		source, err := sourceRepository.Upsert(
			ctx,
			instrument.ID,
			string(ProviderAngelOne),
			mapping.ProviderSymbol,
			&providerID,
			true,
			mapping.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("persist Angel One mapping for %s: %w", definition.CanonicalSymbol, err)
		}
		persisted = append(persisted, source)
	}

	return persisted, nil
}

// normalizeExchange converts the master file's segment names into the venue
// value used by MoneyPlant and by Angel One historical requests.
func normalizeExchange(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "nse", "nse_cm", "nse_cash":
		return "NSE"
	case "bse", "bse_cm", "bse_cash":
		return "BSE"
	default:
		return strings.ToUpper(strings.TrimSpace(value))
	}
}

func containsProviderSymbol(symbols []string, candidate string) bool {
	for _, symbol := range symbols {
		if strings.EqualFold(strings.TrimSpace(symbol), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}
