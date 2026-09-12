# Phase 3.6 — Angel One live message decoder

Phase 3.6 adds the provider-specific binary decoder needed before opening an
Angel One live connection. It does not open a network connection or subscribe
to tokens yet; those responsibilities belong to Phase 3.7.

## SmartAPI packet handling

Angel One WebSocket 2.0 sends market-data responses as little-endian binary
packets. The decoder reads:

- subscription mode (`LTP`, `QUOTE`, or `SNAP_QUOTE`);
- exchange type;
- the fixed-width token field;
- sequence number;
- exchange timestamp in epoch milliseconds;
- last traded price; and
- last traded quantity for Quote and Snap Quote packets.

Prices arrive as scaled integers. NSE prices are converted from paise using
exact decimal text, while currency exchange types use the provider's larger
scale. No value is routed through `float64`.

## Normalized event boundary

`AngelOneMarketDataPacket` keeps the provider fields visible for the upcoming
connection layer. `ToLiveMarketEvent` converts Quote and Snap Quote packets to
MoneyPlant's common `LiveMarketEvent` contract.

LTP packets are decoded successfully, but conversion to a normalized trade
event is rejected because LTP mode does not contain a traded quantity and the
MoneyPlant event contract requires a positive quantity. Phase 3.7 should use
Quote mode for the live subscriptions.

Malformed packets are rejected when they have an unsupported mode, insufficient
length, an empty or invalid token, an invalid timestamp, a non-positive price,
or a non-positive quantity.

## Verification

The offline tests build binary fixture packets and verify:

```bash
go test -run '^TestDecodeAngelOne' ./internal/ingestion -v
```

The tests cover Quote and Snap Quote normalization, exact prices and quantities,
LTP handling, and malformed packet rejection. The official packet layout and
field offsets are documented by [Angel One SmartAPI](https://smartapi.angelone.in/docs/Instruments).
