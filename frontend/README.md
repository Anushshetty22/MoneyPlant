# Frontend

The MoneyPlant Next.js dashboard displays data from the Go REST API.

## Phase 7.1 foundation

This step creates:

- Next.js App Router and TypeScript configuration
- Tailwind CSS styling
- A server-side API client in `src/lib/api.ts`
- A first page that displays active instruments
- Loading-error and empty-data states

## Run locally

Start PostgreSQL and the Go API first:

```bash
cd ../backend
go run ./cmd/api
```

In another terminal:

```bash
cd frontend
cp .env.example .env.local
npm install
npm run typecheck
npm run dev
```

Open `http://localhost:3000` in a browser.

The first page requests `GET /api/v1/instruments` from the Go API and displays
the instruments returned by PostgreSQL. Charts and interactive filters are part
of the next frontend sub-phases.
