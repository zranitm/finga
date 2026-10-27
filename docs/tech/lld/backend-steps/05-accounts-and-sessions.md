# 05. Accounts and sessions

- **Depends on:** 02
- **Features:** A1
- **Milestone:** Foundations
- **Order:** 5
- **Status:** Done
- **Step:** 05. Accounts and sessions

> **Changed by step 24 (2026-10-27):** the account module runs as its own service (`services/account`), reached over gRPC; its revoke hooks feed the `WatchSessions` stream every gateway follows.

**Goal:** sign up, log in, and sessions, with one active session per account.

**Layers in this step:** repository and service. The REST handlers come in step 07.

## Scope

**Shared libraries**

- `pkg/auth`: the `Authenticator` and `Sessions` interfaces and `Principal`, plus `pkg/auth/password` for argon2id hashing in PHC string format.
- `pkg/ratelimit`: keyed token buckets, with idle keys evicted.

**Repository** (`internal/application/account/repository.go`)

- Migration for `accounts` and `sessions` (page 2), and their sqlc queries.
- Insert an account; find one by username, ignoring letter case; insert a session; find an active session by token hash; revoke one session, or every active session of an account; read and write settings.
- The store (`Postgres`): `InTx` with a unit of work over the account and session repositories, plain reads on the pool, and `classify`: `accounts_username_key` becomes `ErrUsernameTaken` (`conflict`), `one_active_session` becomes the module-private `errActiveSession`, and no row becomes `ErrNotFound`.

**Service** (`internal/application/account/service.go`)

- Register: username 3 to 16 characters (letters, digits, underscore), password at least 8, hashed before it reaches the repository.
- Log in: verify credentials, then issue a session in one transaction that revokes the older active session and inserts the new one. The token is 32 random bytes; only its SHA-256 is stored.
- Validate and revoke sessions, with expiry checked against an injected clock. The service implements `pkg/auth`'s interfaces, so handlers depend on the interface, not on this package.
- Login throttling per username and per IP.
- Settings: the control preset must be `classic` or `wasd`.

## Design notes

- The partial unique index on `sessions` guarantees one active session (OD-08). The service revokes first, but the index is the backstop: a violation comes back from `InTx` as `errActiveSession`, and the service runs the login transaction once more.
- Unknown user, wrong password, expired, and revoked all return the same `unauthenticated`, so responses don't reveal which usernames exist.
- Session expiry starts simple: a fixed lifetime. Sliding expiry and rotation come in step 16 (TD-24).

## Tests first

**Repository** (real Postgres, through the store)

- a duplicate username in different letter case gives `ErrUsernameTaken`
- an unknown token hash gives `ErrNotFound`
- revoking every session of an account leaves none active
- a second active session is rejected by the partial unique index and comes back as `errActiveSession`

**Service** (fake store and unit of work, and a fake clock, unless noted)

- register: valid; a table of invalid usernames and passwords; a duplicate becomes `conflict`
- password: correct and wrong passwords; the stored string is argon2id in PHC format
- log in: the token is returned once, and only its hash reaches the repository
- logging in twice: the first session stops validating (real Postgres)
- concurrent logins for the same account leave exactly one active session (real Postgres)
- validate: expired, revoked, and unknown tokens all fail with the same error
- throttling: too many failures give `rate_limited`, which resets after the window
- settings: an unknown preset is rejected

## Done when

- [ ] Repository and service tests pass, including the real-Postgres rows.

## Review focus

- Hash comparisons are constant-time.
- Tokens and passwords never reach a log line.
- The repository makes no decisions: username rules and expiry checks live in the service.
