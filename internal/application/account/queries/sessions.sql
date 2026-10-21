-- name: InsertSession :exec
INSERT INTO sessions (id, account_id, token_hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: FindActiveSessionByTokenHash :one
SELECT id, account_id, token_hash, created_at, expires_at, revoked_at
FROM sessions
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeAccountSessions :execrows
UPDATE sessions SET revoked_at = $2 WHERE account_id = $1 AND revoked_at IS NULL;
