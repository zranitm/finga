-- +goose Up
-- A session ends at its revocation or expiry, whichever came first (LEAST skips a NULL); ended ones are deleted oldest first.
CREATE INDEX sessions_by_end ON sessions (LEAST(revoked_at, expires_at));

-- +goose Down
DROP INDEX sessions_by_end;
