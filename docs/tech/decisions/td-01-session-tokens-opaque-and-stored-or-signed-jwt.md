# TD-01. Session tokens: opaque and stored, or signed JWT?

- **Area:** Security
- **Decision:** TD-01. Session tokens: opaque and stored, or signed JWT?
- **Default:** Opaque and stored
- **Status:** Open

## Options

- **A.** Opaque tokens stored (hashed) in Postgres.
- **B.** Signed JWTs.

## Why the default

Opaque tokens can be revoked instantly, which makes one active session per account (OD-08) simple to enforce.

## Affects

HLD sections 6 and 10; the account module and gateway.
