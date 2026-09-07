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

## Problems and solutions

Add entries here using this format:

```text
Problem:
What I tried:
What worked:
What I learned:
```
