package ingestion

import (
	"errors"
	"strings"
	"testing"
)

// These assertions make the Phase 3.1 contract compile-checkable without
// constructing a provider or contacting a credentialed external service.
var (
	_ HistoricalMarketDataProvider = (*BinanceMarketDataProvider)(nil)
	_ HistoricalMarketDataProvider = (*YahooMarketDataProvider)(nil)
	_ HistoricalMarketDataProvider = (*FixtureMarketDataProvider)(nil)
	_ LiveMarketDataProvider       = (*BinanceLiveMarketDataProvider)(nil)
	_ LiveMarketDataProvider       = (*FixtureLiveMarketDataProvider)(nil)
)

func TestInstrumentReferenceSeparatesCanonicalAndProviderIdentity(t *testing.T) {
	reference := InstrumentReference{
		CanonicalSymbol:      "SBIN",
		Provider:             ProviderYahoo,
		ProviderSymbol:       "SBIN.NS",
		ProviderInstrumentID: "",
	}

	if err := reference.Validate(); err != nil {
		t.Fatalf("valid instrument reference rejected: %v", err)
	}
	if reference.CanonicalSymbol == reference.ProviderSymbol {
		t.Fatal("test must exercise distinct canonical and provider symbols")
	}
}

func TestInstrumentReferenceRejectsLowercaseCanonicalSymbol(t *testing.T) {
	reference := InstrumentReference{
		CanonicalSymbol: "sbin",
		Provider:        ProviderYahoo,
		ProviderSymbol:  "SBIN.NS",
	}

	if err := reference.Validate(); err == nil {
		t.Fatal("lowercase canonical symbol was accepted")
	}
}

func TestProviderCapabilitiesDescribeHistoricalAndLiveSupport(t *testing.T) {
	binanceHistorical := (&BinanceMarketDataProvider{}).Capabilities()
	if !binanceHistorical.Historical || binanceHistorical.Live {
		t.Fatalf("unexpected Binance historical capabilities: %+v", binanceHistorical)
	}
	if !binanceHistorical.SupportsInterval(Interval1d) {
		t.Fatal("Binance should support 1d historical candles")
	}

	yahoo := (&YahooMarketDataProvider{}).Capabilities()
	if !yahoo.SupportsInterval(Interval1d) || yahoo.SupportsInterval(Interval1h) {
		t.Fatalf("unexpected Yahoo capabilities: %+v", yahoo)
	}

	binanceLive := (&BinanceLiveMarketDataProvider{}).Capabilities()
	if binanceLive.Historical || !binanceLive.Live || binanceLive.SupportsInterval(Interval1m) {
		t.Fatalf("unexpected Binance live capabilities: %+v", binanceLive)
	}
}

func TestMarketErrorPreservesProviderContextAndCause(t *testing.T) {
	cause := errors.New("socket closed")
	marketErr := &MarketError{
		Code:            MarketErrorTransport,
		Provider:        ProviderBinance,
		Operation:       "receive live event",
		CanonicalSymbol: "BTCUSDT",
		ProviderSymbol:  "BTCUSDT",
		Retryable:       true,
		Cause:           cause,
	}

	if !errors.Is(marketErr, cause) {
		t.Fatal("MarketError does not unwrap its cause")
	}
	if got := marketErr.Error(); got == "" || !containsAll(got, "transport", "binance", "BTCUSDT", "socket closed") {
		t.Fatalf("MarketError text = %q, missing context", got)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
