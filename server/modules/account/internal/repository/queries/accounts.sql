-- name: InsertAccount :exec
INSERT INTO accounts (id, username, password_hash, settings, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(username), sqlc.arg(password_hash), sqlc.arg(settings), sqlc.arg(now), sqlc.arg(now));

-- name: FindAccountByUsername :one
-- username is citext, so the comparison ignores letter case.
SELECT id, username, password_hash, settings, created_at
FROM accounts
WHERE username = $1;

-- name: LockAccount :one
SELECT id FROM accounts WHERE id = $1 FOR UPDATE;

-- name: GetSettings :one
SELECT settings FROM accounts WHERE id = $1;

-- name: UpdateSettings :execrows
UPDATE accounts SET settings = sqlc.arg(settings), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: ReplacePasswordHash :execrows
-- Only while the hash is still the one verified, so a newer one is never overwritten.
UPDATE accounts SET password_hash = sqlc.arg(new_hash), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id) AND password_hash = sqlc.arg(old_hash);
