-- name: InsertSession :exec
INSERT INTO sessions (id, account_id, token_hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: FindActiveSessionByTokenHash :one
-- Matches the current token or the one retired by the last rotation.
SELECT id, account_id, token_hash, previous_token_hash, rotated_at, created_at, expires_at, revoked_at
FROM sessions
WHERE (token_hash = $1 OR previous_token_hash = $1) AND revoked_at IS NULL;

-- name: FindSessionByID :one
SELECT id, account_id, token_hash, previous_token_hash, rotated_at, created_at, expires_at, revoked_at
FROM sessions
WHERE id = $1;

-- name: RotateSessionToken :execrows
UPDATE sessions SET previous_token_hash = token_hash, token_hash = $2, rotated_at = $3
WHERE id = $1 AND revoked_at IS NULL;

-- name: ConsumePreviousToken :execrows
UPDATE sessions SET rotated_at = NULL WHERE id = $1 AND revoked_at IS NULL;

-- name: ExtendSession :execrows
UPDATE sessions SET expires_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeAccountSessions :execrows
UPDATE sessions SET revoked_at = $2 WHERE account_id = $1 AND revoked_at IS NULL;
