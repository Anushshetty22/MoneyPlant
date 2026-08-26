# Seed data

Place reviewed public macroeconomic CSV files here after documenting their provenance and schema.
## Macro CSV seed format

Phase 4.5 uses the following three-column format for reviewed macroeconomic seed
files:

```text
observed_on,value,source_row_reference
2026-01-01,2.73,RBI DBIE dashboard sample: Jan 2026
```

- `observed_on` is an ISO date in `YYYY-MM-DD` format.
- `value` is the exact decimal observation, not a value converted through `float64`.
- `source_row_reference` records the source row, series note, or review reference.

The `.sample.csv` files are learning fixtures based on the values documented in
the Phase 1 source notes. They are not a complete official RBI export. Replace
them with reviewed official CSV exports before treating them as production seed
data. The Go reader validates the same format for both CPI and policy-rate data.
