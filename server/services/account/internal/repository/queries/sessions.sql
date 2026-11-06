-- name: InsertSession :exec
INSERT INTO sessions (id, account_id, token_hash, expires_at, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(account_id), sqlc.arg(token_hash), sqlc.arg(expires_at), sqlc.arg(now), sqlc.arg(now));

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
UPDATE sessions SET previous_token_hash = token_hash, token_hash = sqlc.arg(token_hash), rotated_at = sqlc.arg(rotated_at), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: ConsumePreviousToken :execrows
UPDATE sessions SET rotated_at = NULL, updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: ExtendSession :execrows
UPDATE sessions SET expires_at = sqlc.arg(expires_at), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeAccountSessions :execrows
UPDATE sessions SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(now) WHERE account_id = sqlc.arg(account_id) AND revoked_at IS NULL;

-- name: DeleteEndedSessions :execrows
-- At most max_rows sessions that ended, revoked or expired, before ended_before; oldest first, by sessions_by_end.
DELETE FROM sessions WHERE id IN (
  SELECT ended.id FROM sessions AS ended
  WHERE LEAST(ended.revoked_at, ended.expires_at) < sqlc.arg(ended_before)::timestamptz
  ORDER BY LEAST(ended.revoked_at, ended.expires_at)
  LIMIT sqlc.arg(max_rows)
);
