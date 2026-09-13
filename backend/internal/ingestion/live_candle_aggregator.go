package ingestion

import (
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

const liveCandleMinuteInterval = "1m"
const liveCandleLateEventWindow = 5 * time.Minute

type liveCandleKey struct {
	sourceID int64
	bucket   time.Time
}

type liveCandleState struct {
	sourceID          int64
	provider          ProviderID
	providerSymbol    string
	bucket            time.Time
	open              pgtype.Numeric
	high              pgtype.Numeric
	low               pgtype.Numeric
	close             pgtype.Numeric
	openAt            time.Time
	closeAt           time.Time
	volume            pgtype.Numeric
	tradeCount        int64
	sourceRetrievedAt time.Time
	flushed           bool
	dirty             bool
}

// LiveMinuteCandleAggregator groups normalized live trades by provider source
// and UTC minute. It keeps only rollup state; raw ticks are never retained.
// Events may arrive out of order within an active bucket because open and close
// are selected by the provider event timestamp, not callback order.
type LiveMinuteCandleAggregator struct {
	mu     sync.Mutex
	states map[liveCandleKey]*liveCandleState
}

// NewLiveMinuteCandleAggregator creates an empty one-minute rollup buffer.
func NewLiveMinuteCandleAggregator() *LiveMinuteCandleAggregator {
	return &LiveMinuteCandleAggregator{states: make(map[liveCandleKey]*liveCandleState)}
}

// Add incorporates one validated event into the source/minute bucket and
// returns its current rollup. Callers can persist the returned candle with the
// existing idempotent market-candle upsert when desired.
func (a *LiveMinuteCandleAggregator) Add(event LiveMarketEvent, instrumentSourceID int64) (database.MarketCandleInput, error) {
	if a == nil {
		return database.MarketCandleInput{}, fmt.Errorf("live candle aggregator is nil")
	}
	if instrumentSourceID <= 0 {
		return database.MarketCandleInput{}, fmt.Errorf("instrument source ID must be positive")
	}
	if err := ValidateLiveMarketEvent(event); err != nil {
		return database.MarketCandleInput{}, fmt.Errorf("aggregate live event: %w", err)
	}
	bucket := event.ObservedAt.UTC().Truncate(time.Minute)
	key := liveCandleKey{sourceID: instrumentSourceID, bucket: bucket}

	a.mu.Lock()
	defer a.mu.Unlock()
	state, exists := a.states[key]
	if !exists {
		state = &liveCandleState{
			sourceID:          instrumentSourceID,
			provider:          event.Provider,
			providerSymbol:    event.ProviderSymbol,
			bucket:            bucket,
			open:              cloneLiveNumeric(event.Price),
			high:              cloneLiveNumeric(event.Price),
			low:               cloneLiveNumeric(event.Price),
			close:             cloneLiveNumeric(event.Price),
			openAt:            event.ObservedAt,
			closeAt:           event.ObservedAt,
			volume:            cloneLiveNumeric(event.Quantity),
			tradeCount:        1,
			sourceRetrievedAt: event.SourceReceivedAt,
			dirty:             true,
		}
		a.states[key] = state
		return state.input(), nil
	}

	if event.ObservedAt.Before(state.openAt) {
		state.openAt = event.ObservedAt
		state.open = cloneLiveNumeric(event.Price)
	}
	if event.ObservedAt.After(state.closeAt) || event.ObservedAt.Equal(state.closeAt) {
		state.closeAt = event.ObservedAt
		state.close = cloneLiveNumeric(event.Price)
	}
	if compareLiveNumeric(event.Price, state.high) > 0 {
		state.high = cloneLiveNumeric(event.Price)
	}
	if compareLiveNumeric(event.Price, state.low) < 0 {
		state.low = cloneLiveNumeric(event.Price)
	}
	state.volume = addLiveNumeric(state.volume, event.Quantity)
	state.tradeCount++
	state.dirty = true
	if event.SourceReceivedAt.After(state.sourceRetrievedAt) {
		state.sourceRetrievedAt = event.SourceReceivedAt
	}
	return state.input(), nil
}

// FlushClosed returns buckets strictly before the supplied event time's minute
// and any recently flushed bucket revised by a late event. It keeps revised
// buckets for a short lateness window so the same database candle can be
// updated instead of creating a duplicate.
func (a *LiveMinuteCandleAggregator) FlushClosed(before time.Time) []database.MarketCandleInput {
	if a == nil {
		return nil
	}
	cutoff := before.UTC().Truncate(time.Minute)
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := make([]liveCandleKey, 0)
	for key, state := range a.states {
		if state.flushed && !state.dirty && cutoff.Sub(key.bucket) > liveCandleLateEventWindow {
			delete(a.states, key)
			continue
		}
		if (state.flushed && state.dirty) || (!state.flushed && key.bucket.Before(cutoff)) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].bucket.Equal(keys[right].bucket) {
			return keys[left].sourceID < keys[right].sourceID
		}
		return keys[left].bucket.Before(keys[right].bucket)
	})
	result := make([]database.MarketCandleInput, 0, len(keys))
	for _, key := range keys {
		state, exists := a.states[key]
		if !exists {
			continue
		}
		result = append(result, state.input())
		state.flushed = true
	}
	return result
}

// MarkPersisted acknowledges a successful upsert for one rollup. Failed
// writes remain dirty and are returned again on a later flush, while a
// successful write is not repeated for every subsequent live event.
func (a *LiveMinuteCandleAggregator) MarkPersisted(candle database.MarketCandleInput) {
	if a == nil {
		return
	}
	key := liveCandleKey{sourceID: candle.InstrumentSourceID, bucket: candle.ObservedAt.Time.UTC().Truncate(time.Minute)}
	a.mu.Lock()
	if state, exists := a.states[key]; exists {
		state.dirty = false
		state.flushed = true
	}
	a.mu.Unlock()
}

// Flush returns and removes every pending rollup, including the active minute.
// Runtime shutdown uses this to persist the current candle before exiting.
func (a *LiveMinuteCandleAggregator) Flush() []database.MarketCandleInput {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := make([]liveCandleKey, 0, len(a.states))
	for key, state := range a.states {
		if !state.flushed || state.dirty {
			keys = append(keys, key)
		}
	}
	result := a.flushKeysLocked(keys)
	// Clean, already-persisted buckets are intentionally retained during normal
	// operation for late-event corrections, but shutdown can release them all.
	a.states = make(map[liveCandleKey]*liveCandleState)
	return result
}

func (a *LiveMinuteCandleAggregator) flushKeysLocked(keys []liveCandleKey) []database.MarketCandleInput {
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].bucket.Equal(keys[right].bucket) {
			return keys[left].sourceID < keys[right].sourceID
		}
		return keys[left].bucket.Before(keys[right].bucket)
	})
	result := make([]database.MarketCandleInput, 0, len(keys))
	for _, key := range keys {
		if state, exists := a.states[key]; exists {
			result = append(result, state.input())
			delete(a.states, key)
		}
	}
	return result
}

func (s *liveCandleState) input() database.MarketCandleInput {
	return database.MarketCandleInput{
		InstrumentSourceID: s.sourceID,
		Interval:           liveCandleMinuteInterval,
		ObservedAt:         pgtype.Timestamptz{Time: s.bucket, Valid: true},
		Open:               cloneLiveNumeric(s.open),
		High:               cloneLiveNumeric(s.high),
		Low:                cloneLiveNumeric(s.low),
		Close:              cloneLiveNumeric(s.close),
		Volume:             cloneLiveNumeric(s.volume),
		TradeCount:         pgtype.Int8{Int64: s.tradeCount, Valid: true},
		SourceRetrievedAt:  pgtype.Timestamptz{Time: s.sourceRetrievedAt, Valid: true},
	}
}

func compareLiveNumeric(left, right pgtype.Numeric) int {
	leftValue, leftErr := finiteNumeric(left, "left")
	rightValue, rightErr := finiteNumeric(right, "right")
	if leftErr != nil || rightErr != nil {
		return 0
	}
	return leftValue.Cmp(rightValue)
}

func addLiveNumeric(left, right pgtype.Numeric) pgtype.Numeric {
	if !left.Valid {
		return cloneLiveNumeric(right)
	}
	if !right.Valid {
		return cloneLiveNumeric(left)
	}
	exponent := left.Exp
	if right.Exp < exponent {
		exponent = right.Exp
	}
	leftInteger := new(big.Int).Set(left.Int)
	rightInteger := new(big.Int).Set(right.Int)
	leftInteger.Mul(leftInteger, livePowerOfTen(left.Exp-exponent))
	rightInteger.Mul(rightInteger, livePowerOfTen(right.Exp-exponent))
	leftInteger.Add(leftInteger, rightInteger)
	return pgtype.Numeric{Int: leftInteger, Exp: exponent, Valid: true}
}

func livePowerOfTen(exponent int32) *big.Int {
	if exponent <= 0 {
		return big.NewInt(1)
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
}
