-- name: InsertCharacter :one
INSERT INTO characters (
  id, account_id, name, class, level, exp,
  strength, dexterity, intelligence, agility, vitality, unspent_points,
  hp, mp, map_id, pos_x, pos_y
) VALUES (
  $1, $2, $3, $4, $5, $6,
  $7, $8, $9, $10, $11, $12,
  $13, $14, $15, $16, $17
)
RETURNING *;

-- name: LockAccountCharacters :exec
-- Serialises one account's creates until the transaction ends.
SELECT pg_advisory_xact_lock(hashtextextended('character.account:' || sqlc.arg(account_id)::text, 0));

-- name: CountCharactersByAccount :one
SELECT count(*) FROM characters WHERE account_id = $1;

-- name: ListCharactersByAccount :many
SELECT * FROM characters WHERE account_id = $1 ORDER BY updated_at, id;

-- name: GetOwnedCharacter :one
SELECT * FROM characters WHERE id = $1 AND account_id = $2;

-- name: SaveCheckpoint :execrows
UPDATE characters
SET map_id = $2, pos_x = $3, pos_y = $4, hp = $5, mp = $6,
    level = $7, exp = $8,
    strength = $9, dexterity = $10, intelligence = $11, agility = $12, vitality = $13
WHERE id = $1;

-- name: SaveLevelUp :execrows
UPDATE characters
SET level = $2, exp = $3,
    strength = $4, dexterity = $5, intelligence = $6, agility = $7, vitality = $8,
    hp = $9, mp = $10
WHERE id = $1;
