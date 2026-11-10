-- name: InsertCharacter :one
INSERT INTO characters (
  id, account_id, name, class, level, exp,
  str_points, dex_points, int_points, agi_points, vit_points, unspent_points, free_reset_used,
  hp, mp, map_id, pos_x, pos_y, created_at, updated_at
) VALUES (
  sqlc.arg(id), sqlc.arg(account_id), sqlc.arg(name), sqlc.arg(class), sqlc.arg(level), sqlc.arg(exp),
  sqlc.arg(str_points), sqlc.arg(dex_points), sqlc.arg(int_points), sqlc.arg(agi_points), sqlc.arg(vit_points), sqlc.arg(unspent_points), sqlc.arg(free_reset_used),
  sqlc.arg(hp), sqlc.arg(mp), sqlc.arg(map_id), sqlc.arg(pos_x), sqlc.arg(pos_y), sqlc.arg(now), sqlc.arg(now)
)
RETURNING *;

-- name: LockAccountCharacters :exec
-- Serialises one account's creates until the transaction ends.
SELECT pg_advisory_xact_lock(hashtextextended('character.account:' || sqlc.arg(account_id)::text, 0));

-- name: CountCharactersByAccount :one
SELECT count(*) FROM characters WHERE account_id = $1;

-- name: ListCharactersByAccount :many
SELECT * FROM characters WHERE account_id = $1 ORDER BY created_at, id;

-- name: GetOwnedCharacter :one
SELECT * FROM characters WHERE id = $1 AND account_id = $2;

-- name: SaveCheckpoint :execrows
UPDATE characters
SET map_id = sqlc.arg(map_id), pos_x = sqlc.arg(pos_x), pos_y = sqlc.arg(pos_y), hp = sqlc.arg(hp), mp = sqlc.arg(mp),
    level = sqlc.arg(level), exp = sqlc.arg(exp),
    str_points = sqlc.arg(str_points), dex_points = sqlc.arg(dex_points), int_points = sqlc.arg(int_points),
    agi_points = sqlc.arg(agi_points), vit_points = sqlc.arg(vit_points), unspent_points = sqlc.arg(unspent_points),
    free_reset_used = sqlc.arg(free_reset_used), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: SaveLevelUp :execrows
UPDATE characters
SET level = sqlc.arg(level), exp = sqlc.arg(exp),
    str_points = sqlc.arg(str_points), dex_points = sqlc.arg(dex_points), int_points = sqlc.arg(int_points),
    agi_points = sqlc.arg(agi_points), vit_points = sqlc.arg(vit_points),
    hp = sqlc.arg(hp), mp = sqlc.arg(mp), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);
