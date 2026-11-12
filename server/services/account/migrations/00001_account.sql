-- +goose Up
-- Case-insensitive text for usernames, which the character service creates too; neither drops it, so rolling one back never breaks the other.
CREATE EXTENSION IF NOT EXISTS citext;

-- The service stamps created_at and updated_at from its own clock: with no default or trigger, a write that leaves one out fails on NOT NULL.
CREATE TABLE accounts (
  id            uuid PRIMARY KEY,
  username      citext NOT NULL UNIQUE,
  password_hash text NOT NULL,                 -- argon2id, PHC string format
  settings      jsonb NOT NULL DEFAULT '{}',   -- control preset (B6)
  created_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL
);

-- previous_token_hash is the token the last rotation retired and rotated_at when, cleared once it is used, so a second use or one past the grace is reuse.
CREATE TABLE sessions (
  id                  uuid PRIMARY KEY,
  account_id          uuid NOT NULL REFERENCES accounts(id),
  token_hash          bytea NOT NULL UNIQUE,       -- SHA-256 of the opaque token
  created_at          timestamptz NOT NULL,
  expires_at          timestamptz NOT NULL,
  revoked_at          timestamptz,
  previous_token_hash bytea,
  rotated_at          timestamptz,
  updated_at          timestamptz NOT NULL
);
-- One active session per account.
CREATE UNIQUE INDEX one_active_session ON sessions (account_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_by_previous_token ON sessions (previous_token_hash) WHERE previous_token_hash IS NOT NULL;
-- A session ends at its revocation or expiry, whichever came first (LEAST skips a NULL); ended ones are deleted oldest first.
CREATE INDEX sessions_by_end ON sessions (LEAST(revoked_at, expires_at));

-- +goose Down
DROP TABLE sessions;
DROP TABLE accounts;
-- citext is left in place: the character service may use it.
