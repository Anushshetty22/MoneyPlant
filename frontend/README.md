# Frontend

The MoneyPlant Next.js dashboard displays data from the Go REST API.

## Phase 7.1 foundation

This step creates:

- Next.js App Router and TypeScript configuration
- Tailwind CSS styling
- A server-side API client in `src/lib/api.ts`
- A first page that displays active instruments
- Loading-error and empty-data states
- A same-origin proxy for browser-side API requests
- Interactive daily closing-price and volume charts

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

The first page requests `GET /api/v1/instruments` from the Go API and then lets
you select an instrument and date range for `GET /api/v1/candles`. The chart is
drawn with SVG so the mapping from API values to screen coordinates is visible.
