-- Phase 2.12 update: latest live snapshots use one upsert per provider symbol,
-- keeping durable state small without storing every raw WebSocket trade.

-- name: UpsertLiveMarketSnapshot :one
INSERT INTO live_market_snapshots (
    provider,
    provider_symbol,
    event_type,
    observed_at,
    price,
    quantity,
    source_received_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (provider, provider_symbol) DO UPDATE SET
    event_type = EXCLUDED.event_type,
    observed_at = EXCLUDED.observed_at,
    price = EXCLUDED.price,
    quantity = EXCLUDED.quantity,
    source_received_at = EXCLUDED.source_received_at,
    updated_at = NOW()
RETURNING
    id,
    provider,
    provider_symbol,
    event_type,
    observed_at,
    price,
    quantity,
    source_received_at,
    created_at,
    updated_at;

-- name: ListLiveMarketSnapshots :many
SELECT
    id,
    provider,
    provider_symbol,
    event_type,
    observed_at,
    price,
    quantity,
    source_received_at,
    created_at,
    updated_at
FROM live_market_snapshots
ORDER BY provider_symbol;
