// Package idempotencypgtest is test support for the idempotency keys' Postgres table.
package idempotencypgtest

// Schema is the key store's table DDL, which a service's migration must match.
const Schema = `
CREATE TABLE idempotency_keys (
  owner        text NOT NULL,
  scope        text NOT NULL,
  key          text NOT NULL,
  request_hash bytea NOT NULL,
  response     bytea NOT NULL,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (owner, scope, key)
);
CREATE INDEX idempotency_keys_by_age ON idempotency_keys (created_at);
`
