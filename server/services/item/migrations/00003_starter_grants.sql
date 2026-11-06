-- +goose Up
-- One row per character given its starter kit, kept for good: GrantStarter runs on every join, so it cannot rest on an idempotency key a cleanup deletes.
CREATE TABLE starter_grants (
  character_id uuid PRIMARY KEY,
  granted_at   timestamptz NOT NULL
);
-- Grants made before this table, known only by their idempotency keys (scope item.starter, key the character ID).
INSERT INTO starter_grants (character_id, granted_at)
SELECT key::uuid, created_at FROM idempotency_keys WHERE scope = 'item.starter'
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE starter_grants;
