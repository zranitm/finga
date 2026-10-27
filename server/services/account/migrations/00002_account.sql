-- +goose Up
-- The account module's tables (data model, page 2).
CREATE TABLE accounts (
  id            uuid PRIMARY KEY,
  username      citext NOT NULL UNIQUE,
  password_hash text NOT NULL,                 -- argon2id, PHC string format
  settings      jsonb NOT NULL DEFAULT '{}',   -- control preset (B6)
  created_at    timestamptz NOT NULL
);

CREATE TABLE sessions (
  id          uuid PRIMARY KEY,
  account_id  uuid NOT NULL REFERENCES accounts(id),
  token_hash  bytea NOT NULL UNIQUE,           -- SHA-256 of the opaque token (TD-01)
  created_at  timestamptz NOT NULL,
  expires_at  timestamptz NOT NULL,
  revoked_at  timestamptz
);
-- one active session per account (OD-08)
CREATE UNIQUE INDEX one_active_session
  ON sessions (account_id) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE sessions;
DROP TABLE accounts;
