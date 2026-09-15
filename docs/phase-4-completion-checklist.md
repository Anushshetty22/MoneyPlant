# Phase 4 Completion Checklist

Phase 4 provides a read-only market analytics layer over stored daily candles.

## Automated verification

- [x] Exact analytics engine tests pass.
- [x] Warm-up, missing-calendar-day, sorting, duplicate, and invalid-close
  behavior is covered.
- [x] Market analytics API tests cover lookback loading, empty results,
  validation, and decimal response values.
- [x] Comparison API tests cover normalized performance output.
- [x] Backend suite passes: `go test ./...`.
- [x] Backend race tests pass for analytics, HTTP, and database packages.
- [x] Frontend typecheck passes: `npm run typecheck`.
- [x] Frontend production build passes: `npm run build`.
- [x] No database migration or derived analytics table is required.

Commands:

```bash
cd backend
go test ./...
go test -race ./internal/analytics ./internal/httpapi ./internal/database

cd ../frontend
npm run typecheck
npm run build
```

## Local acceptance

- [ ] Start PostgreSQL and the Go API.
- [ ] Open the dashboard and select the Analytics section.
- [ ] Select an instrument with stored daily candles.
- [ ] Confirm latest close, total return, drawdown, and volatility appear.
- [ ] Confirm SMA 7/20/50 appear only after enough observations exist.
- [ ] Select two or more instruments with the same provider and confirm the
  normalized comparison chart starts each series at 100.
- [ ] Select a date range with no candles and confirm the clear empty state.
- [ ] Try an invalid or reversed date range and confirm the error state.
- [ ] Confirm the existing Market Data live monitor still works independently.

## Scope boundary

Phase 4 does not include recommendations, buy/sell signals, forecasting,
machine learning, local LLM queries, personal-finance data, macro-market
relationships, real-time analytics, scheduled calculations, or persisted
derived metrics.
