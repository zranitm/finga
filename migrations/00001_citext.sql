-- +goose Up
-- Case-insensitive text for usernames and character names (data model, page 2).
CREATE EXTENSION IF NOT EXISTS citext;

-- +goose Down
DROP EXTENSION IF EXISTS citext;
