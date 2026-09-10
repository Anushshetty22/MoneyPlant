# Phase 3.4 — Angel One authentication

Phase 3.4 adds a credential-safe SmartAPI session client. Credentials are read
from environment variables and are never stored in PostgreSQL, fixtures, or
logs.

Required local variables:

```text
ANGEL_ONE_API_KEY
ANGEL_ONE_CLIENT_CODE
ANGEL_ONE_PASSWORD
ANGEL_ONE_TOTP_SECRET
```

Optional request settings are `ANGEL_ONE_BASE_URL`,
`ANGEL_ONE_CLIENT_LOCAL_IP`, `ANGEL_ONE_CLIENT_PUBLIC_IP`, and
`ANGEL_ONE_MAC_ADDRESS`.

Run the credential check from `backend/`:

```bash
go run ./cmd/auth-angelone
```

The client generates the current six-digit TOTP, calls Angel One’s
`loginByPassword` endpoint, and keeps the returned JWT, refresh token, and feed
token in memory. `Refresh` calls `generateTokens` using the refresh token. The
session type intentionally keeps token fields private and its log formatting
shows only the acquisition timestamp.

Invalid credentials and expired refresh tokens are returned as typed,
provider-aware errors. Response bodies are not logged, and known credential or
token values are redacted from provider messages.
