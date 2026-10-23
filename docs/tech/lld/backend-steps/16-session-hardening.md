# 16. Session hardening

- **Depends on:** 05, 10
- **Features:** A1
- **Milestone:** M6. Proof session
- **Order:** 16
- **Status:** Done
- **Step:** 16. Session hardening

**Goal:** make a stolen session token stop working, and make theft visible. Builds TD-24's current default; if TD-24 changes, this step changes.

**Layers in this step:** all three.

## Scope

**Repository** (account)

- Migration: `sessions` gains `previous_token_hash` and `rotated_at`.
- Find a session by its current or previous token hash; rotate a token; extend `expires_at`; revoke every session of an account.

**Service** (account)

- Sliding expiry: about 7 days idle, extended at most once every 10 minutes, never past 30 days from login.
- Rotation: every Hello gets a new token, and the previous one is accepted once within 60 seconds.
- Reuse detection: any other use of a retired token revokes the session.
- Log out everywhere: revokes every session of the account.
- Username timing leak (carried over from step 05): a login for an unknown username must verify against a dummy argon2id hash, so it takes as long as a wrong password.

**Handler**

- Gateway: Welcome carries the new token. A second Hello on a connected session kicks the first. A timer checks each connection's session and sends Kicked with "session expired". A detected reuse kicks every connection on the session.
- REST: `DELETE /api/sessions` logs out everywhere and kicks the account's connections.
- The Protocol and Data model pages are updated for the new token field and columns.

## Tests first

**Service** (real Postgres, injected clock)

- rotation returns a new token; the old one works once within the grace period, then fails
- reusing a retired token after the grace period revokes the session
- the expiry is extended at most once every 10 minutes, and never past 30 days

**Handler** (fake account service; timers under `synctest`)

- Welcome carries the new token
- a second Hello on a connected session kicks the first
- a connection is kicked when its session expires
- a detected reuse kicks every connection on the session
- logging out everywhere kicks every connection

## Done when

- [ ] Tests pass, and the Trust model section is written on page 4.

## Review focus

- Races between two Hellos carrying the same token.
