-- name: InsertCharacter :exec
INSERT INTO characters (
  id, account_id, name, class, level, exp,
  strength, dexterity, intelligence, agility, vitality, unspent_points,
  hp, mp, map_id, pos_x, pos_y, updated_at
) VALUES (
  $1, $2, $3, $4, $5, $6,
  $7, $8, $9, $10, $11, $12,
  $13, $14, $15, $16, $17, $18
);

-- name: ListCharactersByAccount :many
SELECT * FROM characters WHERE account_id = $1 ORDER BY updated_at, id;

-- name: GetOwnedCharacter :one
SELECT * FROM characters WHERE id = $1 AND account_id = $2;

-- name: SaveCheckpoint :execrows
UPDATE characters
SET map_id = $2, pos_x = $3, pos_y = $4, hp = $5, mp = $6, exp = $7, updated_at = $8
WHERE id = $1;

-- name: SaveLevelUp :execrows
UPDATE characters
SET level = $2, exp = $3,
    strength = $4, dexterity = $5, intelligence = $6, agility = $7, vitality = $8,
    hp = $9, mp = $10, updated_at = $11
WHERE id = $1;
