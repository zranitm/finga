// Package outboxpgtest is test support for the outbox's Postgres table.
package outboxpgtest

// Schema is the outbox writer's table DDL, which a service's migration must match.
const Schema = `
CREATE TABLE outbox (
  id           bigserial PRIMARY KEY,
  event_id     uuid NOT NULL UNIQUE,
  topic        text NOT NULL,
  key          text NOT NULL,
  type         text NOT NULL,
  content_type text NOT NULL,
  payload      bytea NOT NULL,
  occurred_at  timestamptz NOT NULL,
  published_at timestamptz,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL
);
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
CREATE INDEX outbox_published_by_age ON outbox (published_at) WHERE published_at IS NOT NULL;
CREATE INDEX outbox_unpublished_by_age ON outbox (created_at) WHERE published_at IS NULL;
`
