-- MoneyPlant Phase 3.3, migration 009
-- Purpose: add the six canonical Indian-market instruments used by the Angel
-- One catalog. Provider tokens are intentionally not stored here because the
-- current token must be resolved from the latest instrument master.

BEGIN;

INSERT INTO instruments (canonical_symbol, name, asset_type, exchange, currency, metadata)
VALUES
    ('NIFTY50', 'NIFTY 50', 'index', 'NSE', 'INR', '{"catalog":"angel_one"}'::JSONB),
    ('SBIN', 'State Bank of India', 'equity', 'NSE', 'INR', '{"catalog":"angel_one"}'::JSONB),
    ('RELIANCE', 'Reliance Industries', 'equity', 'NSE', 'INR', '{"catalog":"angel_one"}'::JSONB),
    ('TCS', 'Tata Consultancy Services', 'equity', 'NSE', 'INR', '{"catalog":"angel_one"}'::JSONB),
    ('INFY', 'Infosys', 'equity', 'NSE', 'INR', '{"catalog":"angel_one"}'::JSONB),
    ('HDFCBANK', 'HDFC Bank', 'equity', 'NSE', 'INR', '{"catalog":"angel_one"}'::JSONB)
ON CONFLICT (canonical_symbol) DO NOTHING;

COMMIT;
