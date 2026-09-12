package ingestion

import (
	// encoding/binary reads SmartAPI's documented little-endian fields.
	"encoding/binary"
	// fmt creates safe packet validation errors.
	"fmt"
	// strconv formats integer quantities without passing through float64.
	"strconv"
	// strings trims the fixed-width token field and builds exact decimals.
	"strings"
	// time converts the exchange epoch-millisecond timestamp to UTC.
	"time"
	// unicode/utf8 rejects a token field that is not valid text.
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// Angel One SmartAPI WebSocket subscription modes.
	AngelOneSubscriptionModeLTP       byte = 1
	AngelOneSubscriptionModeQuote     byte = 2
	AngelOneSubscriptionModeSnapQuote byte = 3

	angelOneLiveHeaderSize      = 51
	angelOneLiveQuotePacketSize = 123
	angelOneLiveSnapPacketSize  = 379
	angelOneLiveTokenStart      = 2
	angelOneLiveTokenEnd        = 27
	angelOneLiveSequenceStart   = 27
	angelOneLiveTimestampStart  = 35
	angelOneLivePriceStart      = 43
	angelOneLiveQuantityStart   = 51
	angelOneLiveScaleDefault    = int64(100)
	angelOneLiveScaleCurrency   = int64(10000000)
)

// AngelOneMarketDataPacket is one decoded SmartAPI WebSocket binary packet.
// The packet model keeps the provider's exchange type and token visible until
// Phase 3.7 binds a subscribed token to MoneyPlant's canonical instrument.
// Prices and quantities are exact pgtype.Numeric values, not float64 values.
type AngelOneMarketDataPacket struct {
	SubscriptionMode   byte
	ExchangeType       byte
	Token              string
	SequenceNumber     int64
	ExchangeTimestamp  time.Time
	LastTradedPrice    pgtype.Numeric
	LastTradedQuantity pgtype.Numeric
}

// DecodeAngelOneMarketData decodes one SmartAPI binary market-data packet.
// It supports LTP, Quote, and Snap Quote packets. LTP packets contain no last
// traded quantity, so they can be inspected as packets but cannot become a
// normalized MoneyPlant trade event until a quote-mode subscription supplies
// the required quantity.
func DecodeAngelOneMarketData(payload []byte) (AngelOneMarketDataPacket, error) {
	if len(payload) < angelOneLiveHeaderSize {
		return AngelOneMarketDataPacket{}, fmt.Errorf("Angel One market-data packet is too short: got %d bytes, need at least %d", len(payload), angelOneLiveHeaderSize)
	}

	mode := payload[0]
	requiredSize, supported := angelOneLivePacketSize(mode)
	if !supported {
		return AngelOneMarketDataPacket{}, fmt.Errorf("unsupported Angel One subscription mode %d", mode)
	}
	if !validAngelOneExchangeType(payload[1]) {
		return AngelOneMarketDataPacket{}, fmt.Errorf("unsupported Angel One exchange type %d", payload[1])
	}
	if len(payload) < requiredSize {
		return AngelOneMarketDataPacket{}, fmt.Errorf("Angel One mode %d packet is too short: got %d bytes, need at least %d", mode, len(payload), requiredSize)
	}

	token, err := decodeAngelOneToken(payload[angelOneLiveTokenStart:angelOneLiveTokenEnd])
	if err != nil {
		return AngelOneMarketDataPacket{}, err
	}
	exchangeTimestampMillis := int64(binary.LittleEndian.Uint64(payload[angelOneLiveTimestampStart : angelOneLiveTimestampStart+8]))
	if exchangeTimestampMillis <= 0 {
		return AngelOneMarketDataPacket{}, fmt.Errorf("Angel One exchange timestamp must be positive")
	}

	priceRaw := int64(binary.LittleEndian.Uint64(payload[angelOneLivePriceStart : angelOneLivePriceStart+8]))
	price, err := angelOneScaledNumeric(priceRaw, angelOnePriceScale(payload[1]))
	if err != nil {
		return AngelOneMarketDataPacket{}, fmt.Errorf("decode Angel One last traded price: %w", err)
	}

	packet := AngelOneMarketDataPacket{
		SubscriptionMode:  mode,
		ExchangeType:      payload[1],
		Token:             token,
		SequenceNumber:    int64(binary.LittleEndian.Uint64(payload[angelOneLiveSequenceStart : angelOneLiveSequenceStart+8])),
		ExchangeTimestamp: time.UnixMilli(exchangeTimestampMillis).UTC(),
		LastTradedPrice:   price,
	}
	if mode != AngelOneSubscriptionModeLTP {
		quantityRaw := int64(binary.LittleEndian.Uint64(payload[angelOneLiveQuantityStart : angelOneLiveQuantityStart+8]))
		if quantityRaw <= 0 {
			return AngelOneMarketDataPacket{}, fmt.Errorf("Angel One last traded quantity must be positive")
		}
		quantity, err := numericFromString(strconv.FormatInt(quantityRaw, 10))
		if err != nil {
			return AngelOneMarketDataPacket{}, fmt.Errorf("decode Angel One last traded quantity: %w", err)
		}
		packet.LastTradedQuantity = quantity
	}

	return packet, nil
}

// ToLiveMarketEvent converts a decoded packet into MoneyPlant's common live
// event. Quote and Snap Quote packets are required because the common event
// contract requires a positive quantity. LTP packets are intentionally
// rejected here instead of inventing a quantity.
func (p AngelOneMarketDataPacket) ToLiveMarketEvent() (LiveMarketEvent, error) {
	if p.SubscriptionMode == AngelOneSubscriptionModeLTP {
		return LiveMarketEvent{}, fmt.Errorf("Angel One LTP packet has no trade quantity; subscribe in quote mode for MoneyPlant trade events")
	}
	if strings.TrimSpace(p.Token) == "" {
		return LiveMarketEvent{}, fmt.Errorf("Angel One packet token cannot be empty")
	}
	if p.ExchangeTimestamp.IsZero() {
		return LiveMarketEvent{}, fmt.Errorf("Angel One packet exchange timestamp is missing")
	}
	if !p.LastTradedPrice.Valid || !p.LastTradedQuantity.Valid {
		return LiveMarketEvent{}, fmt.Errorf("Angel One packet is missing price or quantity")
	}

	return LiveMarketEvent{
		Provider:         ProviderAngelOne,
		ProviderSymbol:   p.Token,
		EventType:        "trade",
		ObservedAt:       p.ExchangeTimestamp.UTC(),
		Price:            p.LastTradedPrice,
		Quantity:         p.LastTradedQuantity,
		SourceReceivedAt: time.Now().UTC(),
	}, nil
}

func angelOneLivePacketSize(mode byte) (int, bool) {
	switch mode {
	case AngelOneSubscriptionModeLTP:
		return angelOneLiveHeaderSize, true
	case AngelOneSubscriptionModeQuote:
		return angelOneLiveQuotePacketSize, true
	case AngelOneSubscriptionModeSnapQuote:
		return angelOneLiveSnapPacketSize, true
	default:
		return 0, false
	}
}

func decodeAngelOneToken(raw []byte) (string, error) {
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("Angel One packet token is not valid UTF-8")
	}
	if end := strings.IndexByte(string(raw), 0); end >= 0 {
		raw = raw[:end]
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("Angel One packet token is empty")
	}
	return token, nil
}

func angelOnePriceScale(exchangeType byte) int64 {
	if exchangeType == 13 {
		return angelOneLiveScaleCurrency
	}
	return angelOneLiveScaleDefault
}

func validAngelOneExchangeType(exchangeType byte) bool {
	switch exchangeType {
	case 1, 2, 3, 4, 5, 7, 13:
		return true
	default:
		return false
	}
}

func angelOneScaledNumeric(raw, scale int64) (pgtype.Numeric, error) {
	if raw <= 0 {
		return pgtype.Numeric{}, fmt.Errorf("scaled value must be positive")
	}
	whole := raw / scale
	fraction := raw % scale
	width := len(strconv.FormatInt(scale-1, 10))
	return numericFromString(fmt.Sprintf("%d.%0*d", whole, width, fraction))
}
