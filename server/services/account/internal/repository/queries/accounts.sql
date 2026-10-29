-- name: InsertAccount :exec
INSERT INTO accounts (id, username, password_hash, settings, created_at)
VALUES ($1, $2, $3, $4, $5);

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
UPDATE accounts SET settings = $2 WHERE id = $1;
