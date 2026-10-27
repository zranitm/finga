-- +goose Up
-- The transactional outbox (pkg/outbox) and the idempotency keys
-- (pkg/idempotency), in the shape those packages write (data model, page 2).
-- Only the item module uses them, so the item service owns them (page 10).
CREATE TABLE outbox (
  id           bigserial PRIMARY KEY,
  event_id     uuid NOT NULL UNIQUE,
  topic        text NOT NULL,
  key          text NOT NULL,
  type         text NOT NULL,
  content_type text NOT NULL,                  -- application/json now, protobuf in Stage 2
  payload      bytea NOT NULL,
  occurred_at  timestamptz NOT NULL,
  published_at timestamptz                     -- set by the relay (Stage 2)
);
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;

CREATE TABLE idempotency_keys (
  owner        text NOT NULL,                  -- account ID
  scope        text NOT NULL,                  -- command type
  key          text NOT NULL,
  request_hash bytea NOT NULL,
  response     bytea NOT NULL,
  created_at   timestamptz NOT NULL,
  PRIMARY KEY (owner, scope, key)
);

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE outbox;
