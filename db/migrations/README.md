# Database migrations

The numbered SQL files create the MoneyPlant PostgreSQL schema in dependency
order. They are intentionally small and readable so each database change can
be inspected while learning.

## Run with Docker Compose

The Phase 2.1 Compose service mounts this directory at PostgreSQL’s
`/docker-entrypoint-initdb.d` directory. The official PostgreSQL image runs
the files alphabetically when the data volume is empty, so the numeric prefixes
(`001_` through `009_`) are significant:

```text
001 instruments
002 provider mappings
003 market candles
004 macro dataset definitions
005 macro observations
006 ingestion audit records
007 initial definitions
008 latest live market snapshots
009 Angel One canonical instruments
```

This initialization mechanism is suitable for the local learning database. It
is not yet a general-purpose migration runner: the application still has no
schema-version table, and existing volumes do not automatically re-run changed
SQL files. Future schema changes should be added as a new numbered migration
instead of editing an already-applied migration.

For an existing local volume, apply the new Phase 2.12 table explicitly once:

```bash
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -f db/migrations/008_create_live_market_snapshots.sql
```

Then apply the Phase 3.3 canonical-instrument seed:

```bash
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -f db/migrations/009_seed_angel_one_instruments.sql
```
