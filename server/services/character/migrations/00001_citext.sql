-- +goose Up
-- Case-insensitive text for character names, which the account service creates too; neither drops it, so rolling one back never breaks the other.
CREATE EXTENSION IF NOT EXISTS citext;

-- +goose Down
-- Left in place: the other service may use it.
SELECT 1;
