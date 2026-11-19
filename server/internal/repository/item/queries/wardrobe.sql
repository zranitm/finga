-- name: AddPiece :execrows
-- A piece into the account's wardrobe, unless it has it.
INSERT INTO account_pieces (account_id, piece_id, cause_type, cause_ref, item_id, added_at)
VALUES (sqlc.arg(account_id), sqlc.arg(piece_id), sqlc.arg(cause_type), sqlc.narg(cause_ref), sqlc.narg(item_id), sqlc.arg(now))
ON CONFLICT (account_id, piece_id) DO NOTHING;

-- name: ListPieces :many
SELECT piece_id, cause_type, cause_ref, item_id, added_at FROM account_pieces
WHERE account_id = sqlc.arg(account_id)
ORDER BY added_at, piece_id;

-- name: CollectedPieces :many
-- Which of the pieces asked the account has.
SELECT piece_id FROM account_pieces
WHERE account_id = sqlc.arg(account_id) AND piece_id = ANY(sqlc.arg(piece_ids)::text[]);

-- name: FindLook :one
SELECT outfit, headwear, face, weapon, hide_headwear, revision FROM character_looks
WHERE character_id = sqlc.arg(character_id);

-- name: LockLook :one
SELECT outfit, headwear, face, weapon, hide_headwear, revision FROM character_looks
WHERE character_id = sqlc.arg(character_id)
FOR UPDATE;

-- name: SetLook :one
-- The character's look, one revision on.
INSERT INTO character_looks (character_id, outfit, headwear, face, weapon, hide_headwear, revision, created_at, updated_at)
VALUES (sqlc.arg(character_id), sqlc.narg(outfit), sqlc.narg(headwear), sqlc.narg(face), sqlc.narg(weapon), sqlc.arg(hide_headwear), 1, sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (character_id) DO UPDATE SET
  outfit = EXCLUDED.outfit, headwear = EXCLUDED.headwear, face = EXCLUDED.face, weapon = EXCLUDED.weapon,
  hide_headwear = EXCLUDED.hide_headwear, revision = character_looks.revision + 1, updated_at = EXCLUDED.updated_at
RETURNING revision;
