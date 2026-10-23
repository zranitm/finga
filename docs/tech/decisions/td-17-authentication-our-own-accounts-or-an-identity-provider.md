# TD-17. Authentication: our own accounts or an identity provider?

- **Area:** Security
- **Decision:** TD-17. Authentication: our own accounts or an identity provider?
- **Default:** Own account module
- **Outcome:** Own account module in Postgres for now, behind a pkg/auth interface. Keycloak (or Ory Kratos or Zitadel) can replace it later without touching the gateway.
- **Status:** Decided

## Options considered

- **Own account module:** username and password, argon2id hashing, opaque sessions in Postgres.
- **Keycloak:** full identity system with OpenID Connect, single sign-on, social login, and two-factor authentication. Heavy to run, and its JWT sessions make one session per account harder.
- **Ory Kratos or Zitadel:** lighter identity services written in Go.

## Why

- The product needs only a username and password (A1).
- The gateway needs one active session per account (OD-08) and instant revocation, which is simplest with our own session table (TD-01).
- No extra service on the VPS.

## Revisit when

Email, password reset, social login, or two-factor authentication enter the product scope.
