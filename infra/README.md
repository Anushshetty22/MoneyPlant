# Infrastructure

This directory contains the local Docker Compose definition for PostgreSQL.
The backend and frontend remain developer-run processes in Phase 2.1 so their
Go and Next.js feedback loops stay simple while the database is containerized.

## Start PostgreSQL

Run these commands from the repository root:

```bash
docker compose -f infra/compose.yaml up -d
docker compose -f infra/compose.yaml ps
```

The service uses the documented local defaults:

- container: `moneyplant-postgres`
- database: `moneyplant`
- user: `moneyplant`
- host port: `5432`
- volume: `moneyplant-postgres-data`

### Moving from the pre-Compose container

If `docker ps` already shows an unmanaged container named
`moneyplant-postgres`, leave it running while you finish any current work;
starting Compose at the same time will fail because the name and host port are
already in use. The new Compose file uses the same PostgreSQL 18 parent-volume
layout, so the existing named volume can be preserved during a deliberate
handoff:

```bash
docker stop moneyplant-postgres
docker rm moneyplant-postgres
docker compose -f infra/compose.yaml up -d
```

The `docker rm` command removes only the old container, not the named volume.
Verify the volume name with `docker inspect moneyplant-postgres` before this
handoff, and do not use `docker compose down -v` because that removes the local
database volume.

On the first start of a new volume, PostgreSQL executes the numbered files in
`db/migrations/` in filename order. Migration `007_seed_initial_definitions.sql`
also inserts the initial instruments and macro-dataset definitions. Because the
official PostgreSQL image only runs initialization files for an empty data
directory, later `up` commands preserve the existing schema and rows.

Check startup details with:

```bash
docker compose -f infra/compose.yaml logs postgres
PGPASSWORD=change-me-locally psql \
  -h localhost -p 5432 -U moneyplant -d moneyplant \
  -c "SELECT canonical_symbol FROM instruments ORDER BY id;"
```

Stop the container while keeping the data volume:

```bash
docker compose -f infra/compose.yaml stop
```

Restart it later with `docker compose -f infra/compose.yaml start`.

To remove the container but preserve data, use `docker compose -f
infra/compose.yaml down`. Treat volume removal as a deliberate destructive
operation because it deletes the local learning database and requires the
migrations to run again.
