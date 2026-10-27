-- +goose Up
-- Case-insensitive text for character names (data model, page 2). The account and
-- character services both need it and both create it; neither drops it, so
-- rolling one service back never breaks the other.
CREATE EXTENSION IF NOT EXISTS citext;

-- +goose Down
-- Left in place: the other service may use it.
SELECT 1;
