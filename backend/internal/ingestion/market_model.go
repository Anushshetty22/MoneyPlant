package ingestion

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ProviderID is MoneyPlant's stable identifier for an external market-data
// provider. It is deliberately separate from a provider's symbol or token.
type ProviderID string

const (
	ProviderBinance  ProviderID = "binance"
	ProviderYahoo    ProviderID = "yahoo"
	ProviderAngelOne ProviderID = "angel_one"
	ProviderFixture  ProviderID = "fixture"
)

// MarketInterval is the normalized interval vocabulary shared by historical
// providers and the market_candles table.
type MarketInterval string

const (
	Interval1m  MarketInterval = "1m"
	Interval5m  MarketInterval = "5m"
	Interval15m MarketInterval = "15m"
	Interval30m MarketInterval = "30m"
	Interval1h  MarketInterval = "1h"
	Interval4h  MarketInterval = "4h"
	Interval1d  MarketInterval = "1d"
	Interval1w  MarketInterval = "1w"
)

// AllMarketIntervals returns a fresh slice so callers cannot mutate the
// shared vocabulary used by provider capability descriptions.
func AllMarketIntervals() []MarketInterval {
	return []MarketInterval{
		Interval1m,
		Interval5m,
		Interval15m,
		Interval30m,
		Interval1h,
		Interval4h,
		Interval1d,
		Interval1w,
	}
}

// InstrumentReference connects MoneyPlant's canonical identity to one exact
// provider representation. CanonicalSymbol is stable inside MoneyPlant;
// ProviderSymbol and ProviderInstrumentID belong to the external provider.
type InstrumentReference struct {
	CanonicalSymbol      string
	Provider             ProviderID
	ProviderSymbol       string
	ProviderInstrumentID string
}

// Validate checks the identity fields that must be known before a provider
// request or normalized event can be routed safely.
func (r InstrumentReference) Validate() error {
	if strings.TrimSpace(r.CanonicalSymbol) == "" {
		return errors.New("canonical symbol cannot be empty")
	}
	if r.CanonicalSymbol != strings.ToUpper(r.CanonicalSymbol) {
		return fmt.Errorf("canonical symbol %q must be uppercase", r.CanonicalSymbol)
	}
	if strings.TrimSpace(string(r.Provider)) == "" {
		return errors.New("provider cannot be empty")
	}
	if string(r.Provider) != strings.ToLower(string(r.Provider)) {
		return fmt.Errorf("provider %q must be lowercase", r.Provider)
	}
	if strings.TrimSpace(r.ProviderSymbol) == "" {
		return errors.New("provider symbol cannot be empty")
	}
	return nil
}

// ProviderCapabilities describes what one provider adapter can do. Supported
// intervals apply to historical candles; live trade streams are event-based
// and therefore do not need an interval value.
type ProviderCapabilities struct {
	Historical         bool
	Live               bool
	SupportedIntervals []MarketInterval
}

// SupportsInterval reports whether the capability description includes an
// interval. A provider with no historical capability supports no intervals.
func (c ProviderCapabilities) SupportsInterval(interval MarketInterval) bool {
	if !c.Historical {
		return false
	}
	for _, supported := range c.SupportedIntervals {
		if supported == interval {
			return true
		}
	}
	return false
}

// MarketStatusState is the common lifecycle vocabulary for provider monitors.
// The existing live status endpoint keeps its wire-compatible string fields;
// this type is the shared model future multi-provider status will use.
type MarketStatusState string

const (
	MarketStatusDisabled     MarketStatusState = "disabled"
	MarketStatusStarting     MarketStatusState = "starting"
	MarketStatusRunning      MarketStatusState = "running"
	MarketStatusReconnecting MarketStatusState = "reconnecting"
	MarketStatusStopped      MarketStatusState = "stopped"
	MarketStatusError        MarketStatusState = "error"
)

// ProviderMarketStatus is the provider-aware status shape shared by future
// monitors. It intentionally identifies both canonical and provider symbols.
type ProviderMarketStatus struct {
	Instrument InstrumentReference
	State      MarketStatusState
	Received   int64
	Accepted   int64
	Rejected   int64
	Reconnects int64
	LastError  *MarketError
	UpdatedAt  time.Time
}

// MarketErrorCode classifies failures consistently across providers and
// storage boundaries. The code is safe to expose in logs and API responses;
// the wrapped cause may contain provider-specific detail.
type MarketErrorCode string

const (
	MarketErrorConfiguration  MarketErrorCode = "configuration"
	MarketErrorAuthentication MarketErrorCode = "authentication"
	MarketErrorAuthorization  MarketErrorCode = "authorization"
	MarketErrorRateLimit      MarketErrorCode = "rate_limit"
	MarketErrorTransport      MarketErrorCode = "transport"
	MarketErrorDecode         MarketErrorCode = "decode"
	MarketErrorValidation     MarketErrorCode = "validation"
	MarketErrorUnsupported    MarketErrorCode = "unsupported"
	MarketErrorPersistence    MarketErrorCode = "persistence"
	MarketErrorUnknown        MarketErrorCode = "unknown"
)

// MarketError is the common error envelope for provider, normalization, and
// persistence failures. Retryable is a policy hint, not an automatic retry.
type MarketError struct {
	Code            MarketErrorCode
	Provider        ProviderID
	Operation       string
	CanonicalSymbol string
	ProviderSymbol  string
	Retryable       bool
	Cause           error
}

func (e *MarketError) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := string(e.Code)
	if message == "" {
		message = string(MarketErrorUnknown)
	}
	if e.Operation != "" {
		message += " " + e.Operation
	}
	if e.Provider != "" {
		message += " for provider " + string(e.Provider)
	}
	if e.ProviderSymbol != "" {
		message += " symbol " + e.ProviderSymbol
	}
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

// Unwrap preserves errors.Is/errors.As behavior for provider-specific causes.
func (e *MarketError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
