# Phase 3.13 End-to-End Verification

Phase 3.13 is the final verification and documentation pass for the unified
multi-source market platform. Automated tests use local HTTP/WebSocket fakes
and deterministic fixtures, so they do not require credentials or an open
market. Real acceptance is a separate local run with PostgreSQL and provider
credentials.

## What is verified in code

The provider adapter tests verify that raw Binance and Angel One messages are
decoded and mapped to the common live-event contract. The Phase 3 integration
tests then run normalized Binance and Angel One events through the same monitor,
latest-snapshot store, and one-minute candle aggregator. HTTP tests cover the
provider-aware API and filtered SSE stream.

This split is deliberate: the adapter owns provider protocol details, while
the monitor, storage, API, and dashboard stay provider-independent.

## Local acceptance sequence

1. Start PostgreSQL and apply migrations.
2. Configure local Angel One credentials in `.env`; keep the file untracked.
3. Refresh the Angel One instrument catalog.
4. Start the API with the Binance and Angel One monitor symbol lists enabled.
5. Inspect `/api/v1/live/statuses` and wait for accepted events.
6. Open the dashboard and verify its source label, latest value, and freshness
   state for both provider types.
7. Open one filtered SSE URL and confirm snapshot, status, and heartbeat events.
8. Stop and restart the API. Confirm durable snapshots are restored, then wait
   for new events and a completed one-minute candle.

The exact commands and checkboxes are maintained in
[`phase-3-completion-checklist.md`](phase-3-completion-checklist.md). The
complete Angel One setup, catalog refresh, and restart details remain in the
[`ingestion-runbook.md`](ingestion-runbook.md).
