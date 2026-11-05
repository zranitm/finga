package idempotency

// Schema is Store's table DDL, which a service's migration must match.
const Schema = `
CREATE TABLE idempotency_keys (
  owner        text NOT NULL,
  scope        text NOT NULL,
  key          text NOT NULL,
  request_hash bytea NOT NULL,
  response     bytea NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner, scope, key)
);
`
