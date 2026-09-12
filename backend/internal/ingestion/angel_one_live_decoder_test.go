package ingestion_test

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

func TestDecodeAngelOneQuotePacketToLiveEvent(t *testing.T) {
	payload := angelOnePacket(
		ingestion.AngelOneSubscriptionModeQuote,
		1,
		"99926000",
		1788241500123,
		1234567,
		7,
		123,
	)

	packet, err := ingestion.DecodeAngelOneMarketData(payload)
	if err != nil {
		t.Fatalf("decode Angel One quote packet: %v", err)
	}
	if packet.Token != "99926000" || packet.ExchangeType != 1 || packet.SubscriptionMode != ingestion.AngelOneSubscriptionModeQuote {
		t.Fatalf("packet identity = %#v", packet)
	}
	if !packet.ExchangeTimestamp.Equal(time.UnixMilli(1788241500123).UTC()) {
		t.Errorf("exchange timestamp = %s", packet.ExchangeTimestamp)
	}

	event, err := packet.ToLiveMarketEvent()
	if err != nil {
		t.Fatalf("convert packet to live event: %v", err)
	}
	if err := ingestion.ValidateLiveMarketEvent(event); err != nil {
		t.Fatalf("validate normalized event: %v", err)
	}
	if event.Provider != ingestion.ProviderAngelOne || event.ProviderSymbol != "99926000" || event.EventType != "trade" {
		t.Fatalf("event identity = %#v", event)
	}
	encodedPrice, err := json.Marshal(event.Price)
	if err != nil {
		t.Fatalf("marshal exact price: %v", err)
	}
	if string(encodedPrice) != "12345.67" {
		t.Errorf("price = %s, want 12345.67", encodedPrice)
	}
	encodedQuantity, err := json.Marshal(event.Quantity)
	if err != nil {
		t.Fatalf("marshal exact quantity: %v", err)
	}
	if string(encodedQuantity) != "7" {
		t.Errorf("quantity = %s, want 7", encodedQuantity)
	}
}

func TestDecodeAngelOneSnapQuotePacket(t *testing.T) {
	packet, err := ingestion.DecodeAngelOneMarketData(angelOnePacket(
		ingestion.AngelOneSubscriptionModeSnapQuote,
		1,
		"3045",
		1788241500123,
		19450,
		120,
		379,
	))
	if err != nil {
		t.Fatalf("decode Snap Quote packet: %v", err)
	}
	if packet.Token != "3045" || !packet.LastTradedQuantity.Valid {
		t.Fatalf("snap packet = %#v", packet)
	}
}

func TestDecodeAngelOneLTPPacketRequiresQuoteForNormalizedEvent(t *testing.T) {
	packet, err := ingestion.DecodeAngelOneMarketData(angelOnePacket(
		ingestion.AngelOneSubscriptionModeLTP,
		1,
		"99926000",
		1788241500123,
		1234567,
		0,
		51,
	))
	if err != nil {
		t.Fatalf("decode LTP packet: %v", err)
	}
	if packet.LastTradedQuantity.Valid {
		t.Fatal("LTP packet unexpectedly contains a quantity")
	}
	if _, err := packet.ToLiveMarketEvent(); err == nil || !strings.Contains(err.Error(), "subscribe in quote mode") {
		t.Fatalf("LTP conversion error = %v", err)
	}
}

func TestDecodeAngelOnePacketRejectsMalformedData(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    string
	}{
		{name: "too short", payload: make([]byte, 50), want: "too short"},
		{name: "unsupported mode", payload: angelOnePacket(4, 1, "99926000", 1788241500123, 10000, 1, 51), want: "unsupported Angel One subscription mode"},
		{name: "unsupported exchange", payload: angelOnePacket(1, 6, "99926000", 1788241500123, 10000, 1, 51), want: "unsupported Angel One exchange type"},
		{name: "empty token", payload: angelOnePacket(1, 1, "", 1788241500123, 10000, 1, 51), want: "token is empty"},
		{name: "zero timestamp", payload: angelOnePacket(1, 1, "99926000", 0, 10000, 1, 51), want: "timestamp must be positive"},
		{name: "zero quantity", payload: angelOnePacket(2, 1, "99926000", 1788241500123, 10000, 0, 123), want: "quantity must be positive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ingestion.DecodeAngelOneMarketData(test.payload)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func angelOnePacket(mode, exchangeType byte, token string, timestamp, price, quantity int64, size int) []byte {
	payload := make([]byte, size)
	payload[0] = mode
	payload[1] = exchangeType
	copy(payload[2:27], token)
	binary.LittleEndian.PutUint64(payload[27:35], uint64(123))
	binary.LittleEndian.PutUint64(payload[35:43], uint64(timestamp))
	binary.LittleEndian.PutUint64(payload[43:51], uint64(price))
	if size >= 59 {
		binary.LittleEndian.PutUint64(payload[51:59], uint64(quantity))
	}
	return payload
}
