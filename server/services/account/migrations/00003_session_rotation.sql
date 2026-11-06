-- +goose Up
-- Session rotation: previous_token_hash is the token the last rotation retired and rotated_at when, cleared once it is used, so a second use or one past the grace is reuse.
ALTER TABLE sessions
  ADD COLUMN previous_token_hash bytea,
  ADD COLUMN rotated_at timestamptz;
CREATE INDEX sessions_by_previous_token
  ON sessions (previous_token_hash) WHERE previous_token_hash IS NOT NULL;

-- +goose Down
DROP INDEX sessions_by_previous_token;
ALTER TABLE sessions DROP COLUMN previous_token_hash, DROP COLUMN rotated_at;
