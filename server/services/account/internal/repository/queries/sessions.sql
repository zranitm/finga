-- name: InsertSession :exec
INSERT INTO sessions (id, account_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4);

-- name: FindActiveSessionByTokenHash :one
-- Matches the current token or the one retired by the last rotation.
SELECT id, account_id, token_hash, previous_token_hash, rotated_at, created_at, expires_at, revoked_at
FROM sessions
WHERE (token_hash = $1 OR previous_token_hash = $1) AND revoked_at IS NULL;

-- name: FindSessionByID :one
SELECT id, account_id, token_hash, previous_token_hash, rotated_at, created_at, expires_at, revoked_at
FROM sessions
WHERE id = $1;

-- name: LockSession :one
SELECT id, account_id, token_hash, previous_token_hash, rotated_at, created_at, expires_at, revoked_at
FROM sessions
WHERE id = $1
FOR UPDATE;

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

-- name: DeleteEndedSessions :execrows
-- At most max_rows sessions that ended, revoked or expired, before ended_before; oldest first, by sessions_by_end.
DELETE FROM sessions WHERE id IN (
  SELECT ended.id FROM sessions AS ended
  WHERE LEAST(ended.revoked_at, ended.expires_at) < sqlc.arg(ended_before)::timestamptz
  ORDER BY LEAST(ended.revoked_at, ended.expires_at)
  LIMIT sqlc.arg(max_rows)
);
