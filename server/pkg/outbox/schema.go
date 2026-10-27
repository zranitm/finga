package outbox

// Schema is the table Writer writes, as DDL. A service that uses the outbox
// creates it in one of its own migrations, and this package's tests create
// it from here; a test in that service checks the two agree.
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
  published_at timestamptz
);
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
`
