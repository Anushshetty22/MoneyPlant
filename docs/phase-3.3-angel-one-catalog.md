# Phase 3.3 — Angel One instrument catalog

Phase 3.3 adds the credential-free catalog layer needed before Angel One
historical or live requests can be implemented.

The application keeps two identities separate:

- canonical symbols are stable MoneyPlant values: `NIFTY50`, `SBIN`,
  `RELIANCE`, `TCS`, `INFY`, and `HDFCBANK`;
- provider symbols and tokens come from the current Angel One instrument master.

The parser accepts the JSON array published by Angel One. It reads the exchange
segment, trading symbol, and token, then resolves the six configured canonical
instruments. `nse_cm` is normalized to `NSE`; the original segment and other
provider details are retained in `instrument_sources.metadata`.

## Local catalog refresh

No Angel One credentials are needed for this step. After the database has been
initialized through migration `009_seed_angel_one_instruments.sql`, run:

```bash
cd backend
go run ./cmd/catalog-angelone --file testdata/angel_one_instrument_master.json
```

For a real refresh, replace the fixture path with the latest local
`OpenAPIScripMaster.json` download. The command resolves the current token and
upserts the mapping, so tokens are never hardcoded in migrations or Go code.

The resolver fails with a readable error when a required mapping is missing or
when more than one master row matches the same canonical instrument. A fixture
is included for tests and local learning; its tokens are deliberately not
production identifiers.
