# MoneyPlant Learning Notes

Use this file as a running project notebook. Keep entries short and practical.

## Concepts to learn

- Git and repository hygiene
- Docker images, containers, volumes, ports, and networks
- Go modules, packages, structs, interfaces, and error handling
- HTTP requests and JSON decoding
- PostgreSQL tables, keys, constraints, indexes, and migrations
- Batch pipelines and idempotency
- REST API design
- Next.js App Router and client-side data fetching
- Time-series data and timezone normalization
- WebSocket streams, context cancellation, callbacks, and reconnect boundaries

## Commands learned

| Command | What it does | Example |
|---|---|---|
| `git status` | Shows repository changes | `git status --short` |
| `go version` | Shows the installed Go version | `go version` |
| `node --version` | Shows the installed Node.js version | `node --version` |
| `docker compose ...` | Starts or inspects the local multi-container setup | `docker compose -f infra/compose.yaml up -d` |

## Decision log

| Date | Decision | Reason |
|---|---|---|
| 2026-08-07 | Start with a small configurable data universe | Easier to test and understand before scaling |
| 2026-08-07 | Research data sources before finalizing the schema | Source shape and limitations affect the warehouse design |
| 2026-09-06 | Use PostgreSQL initialization mounts for local Phase 2.1 setup | The numbered migrations already express dependency order, and a named volume preserves local data across restarts |
| 2026-09-06 | Define and test a provider-neutral live stream before adding Binance WebSocket transport | A fixture can prove event validation and cancellation without credentials, network access, or provider protocol details |
| 2026-09-07 | Map Binance's raw `<symbol>@trade` stream into the normalized event contract | The adapter owns lowercase URL symbols and provider JSON fields while the monitor stays provider-independent |
| 2026-09-07 | Add bounded reconnects around the live stream instead of embedding retries in the Binance decoder | Transport recovery, event decoding, and event handling remain separate responsibilities and can be tested independently |
| 2026-09-07 | Keep the latest live event in a locked in-memory snapshot store before designing durable tick storage | This makes live state observable while keeping high-volume streaming data separate from the Phase 1 historical candle schema |
| 2026-09-08 | Add a bounded `monitor-binance` command before adding persistence or API endpoints | A runnable command makes the complete live flow testable while keeping database and HTTP design decisions separate |
| 2026-09-08 | Expose live snapshots through the existing API only when `LIVE_MONITOR_SYMBOL` is set | The Phase 1 read API remains usable without a WebSocket, while live monitoring becomes observable through a normal HTTP request |
| 2026-09-08 | Poll the live snapshot endpoint from the dashboard instead of opening a browser WebSocket | The backend owns the Binance connection, while the first frontend integration stays simple and reuses the existing same-origin API proxy |
| 2026-09-08 | Mark a live snapshot stale after three dashboard polling intervals without a new backend timestamp | Operational monitoring must distinguish a previously received value from a currently healthy stream |
| 2026-09-08 | Keep monitor lifecycle counters in a separate status store and expose them through `/api/v1/live/status` | Market snapshots answer “what was the latest value?” while status metadata answers “is the monitor working?” |
| 2026-09-08 | Display backend monitor state and counters in the dashboard | A frontend should consume explicit operational status rather than infer every failure from missing market values |
| 2026-09-08 | Count reconnect attempts through an optional retry callback | Recovery can be healthy overall while still revealing transport instability that operators should observe |
| 2026-09-08 | Persist one latest live snapshot per provider symbol instead of every raw trade | Restart safety is useful, but high-volume tick history needs a separate retention and storage design |
| 2026-09-09 | Track persistence successes and errors separately from live-stream health | A connected provider does not guarantee that the latest value was saved successfully |
| 2026-09-09 | Report how many durable snapshots were restored at startup | Recovery behavior should be visible instead of inferred from a value that happens to appear after restart |

## Problems and solutions

Add entries here using this format:

```text
Problem:
What I tried:
What worked:
What I learned:
```
