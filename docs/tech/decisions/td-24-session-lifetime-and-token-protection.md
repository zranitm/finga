# TD-24. Session lifetime and token protection

- **Area:** Security
- **Decision:** TD-24. Session lifetime and token protection
- **Default:** Sliding expiry with a 30-day cap, token rotation on every Hello with reuse detection, one connection per session, log out everywhere
- **Status:** Open

## Proposal

- **Sliding expiry:** about 7 days idle. Each Hello, REST call, or open connection pushes `expires_at` forward, at most once every 10 minutes per connection.
- **Absolute lifetime:** 30 days from login. Extension never goes past it.
- **Rotation with reuse detection:** every Hello issues a new token in Welcome and retires the old one. The previous token is accepted once within about 60 seconds, for a Welcome lost on a bad connection. Any later use of a retired token revokes the whole session and kicks every connection on it.
- **One connection per session:** a second Hello on an already-connected session kicks the first.
- **Expiry while connected:** the gateway checks on a timer and sends Kicked with "session expired".
- **Log out everywhere:** revokes every session of the account.
- **Later:** password re-entry for sensitive actions, such as changing the password or large market trades.

## Why

Sliding expiry is mainly convenience and housekeeping. Rotation with reuse detection is what makes a stolen token stop working, and it makes theft visible.

## Affects

Backend implementation, step 16; Data model (`sessions` gains `previous_token_hash` and `rotated_at`); Protocol (Welcome carries the new token).
