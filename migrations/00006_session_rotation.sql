-- +goose Up
-- Session rotation and reuse detection (step 16, TD-24's default).
-- previous_token_hash is the token retired by the last rotation; rotated_at
-- is when, and it is cleared once the previous token has been used, so a
-- second use (or a use after the grace) is reuse.
ALTER TABLE sessions
  ADD COLUMN previous_token_hash bytea,
  ADD COLUMN rotated_at timestamptz;
CREATE INDEX sessions_by_previous_token
  ON sessions (previous_token_hash) WHERE previous_token_hash IS NOT NULL;

-- +goose Down
DROP INDEX sessions_by_previous_token;
ALTER TABLE sessions DROP COLUMN previous_token_hash, DROP COLUMN rotated_at;
