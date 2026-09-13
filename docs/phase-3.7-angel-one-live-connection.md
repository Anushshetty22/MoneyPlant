# Phase 3.7 — Angel One live connection

Phase 3.7 adds the authenticated Angel One SmartAPI WebSocket connection on top
of the Phase 3.6 binary decoder. It opens one multiplexed connection for a set
of provider tokens instead of opening one socket per instrument.

## Connection behavior

- The socket uses the SmartAPI WebSocket 2.0 endpoint and the required
  `Authorization`, `x-api-key`, `x-client-code`, and `x-feed-token` headers.
- Subscriptions are sent as one Quote-mode request, grouped by exchange type.
  Quote mode is intentional because MoneyPlant's normalized live event requires
  both price and quantity; LTP packets do not include quantity.
- The stream sends the provider heartbeat text `ping` every 30 seconds and
  handles `pong` responses. A received `ping` receives a `pong` reply.
- Binary packets are passed through the Phase 3.6 decoder. Unknown tokens and
  malformed market packets are skipped so one bad event does not terminate a
  healthy multiplexed connection.
- A rejected WebSocket handshake triggers an in-memory refresh-token attempt;
  an expired refresh token triggers a fresh login. Tokens are never logged.
- `MultiplexedProvider` adapts the fixed token set to the existing
  `ReconnectingLiveMarketStream`, so the shared bounded backoff policy can
  reopen the socket after a disconnect.

## Provider usage

The provider-specific subscription shape is:

```go
subscriptions := []ingestion.AngelOneLiveSubscription{
    {
        CanonicalSymbol:      "NIFTY50",
        ProviderSymbol:       "Nifty 50",
        ProviderInstrumentID: "99926000",
        ExchangeType:         1, // NSE cash
    },
}

provider, _ := ingestion.NewAngelOneLiveMarketDataProvider(
    nil,
    authenticator,
    session,
    ingestion.AngelOneSmartStreamURL,
)

stream, _ := provider.OpenMultiplexedTradeStream(ctx, subscriptions)
```

For reconnects, use `provider.MultiplexedProvider(subscriptions)` with the
existing `NewReconnectingLiveMarketStream` wrapper.

## Verification

Run the focused tests from `backend/`:

```bash
go test -run '^TestAngelOneLiveProvider' ./internal/ingestion -v
go test -race -run '^TestAngelOneLiveProvider' ./internal/ingestion
```

The tests verify one six-token connection, authenticated headers, Quote-mode
subscription JSON, binary event mapping, malformed-event tolerance, and
reconnection after a socket closes. The official SmartAPI documentation defines
the connection headers, heartbeat, subscription contract, and binary response
layout: [Angel One SmartAPI WebSocket documentation](https://smartapi.angelone.in/docs/WebSocketOrderStatus).
