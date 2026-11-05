package outbox

// Schema is Writer's table DDL, which a service's migration must match.
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
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
`
