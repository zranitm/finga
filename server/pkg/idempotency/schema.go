package idempotency

// Schema is the table Store reads and writes, as DDL. A service that uses
// idempotency keys creates it in one of its own migrations, and this
// package's tests create it from here; a test in that service checks the
// two agree.
const Schema = `
CREATE TABLE idempotency_keys (
  owner        text NOT NULL,
  scope        text NOT NULL,
  key          text NOT NULL,
  request_hash bytea NOT NULL,
  response     bytea NOT NULL,
  created_at   timestamptz NOT NULL,
  PRIMARY KEY (owner, scope, key)
);
`
