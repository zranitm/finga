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
    free_reset_used = sqlc.arg(free_reset_used), unspent_skill_points = sqlc.arg(unspent_skill_points), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: SaveLevelUp :execrows
UPDATE characters
SET level = sqlc.arg(level), exp = sqlc.arg(exp),
    str_points = sqlc.arg(str_points), dex_points = sqlc.arg(dex_points), int_points = sqlc.arg(int_points),
    agi_points = sqlc.arg(agi_points), vit_points = sqlc.arg(vit_points),
    hp = sqlc.arg(hp), mp = sqlc.arg(mp), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: ListQuests :many
SELECT quest_id, status, progress, times_finished FROM character_quests WHERE character_id = $1 ORDER BY quest_id;

-- name: SaveQuests :exec
-- Each quest's progress comes as its counts joined by commas, as arrays of arrays can't be ragged; a row already as given is left alone.
INSERT INTO character_quests (character_id, quest_id, status, progress, times_finished, created_at, updated_at)
SELECT sqlc.arg(character_id), (sqlc.arg(quest_ids)::text[])[k], (sqlc.arg(statuses)::text[])[k],
       string_to_array((sqlc.arg(progress)::text[])[k], ',')::int[], (sqlc.arg(times_finished)::int[])[k], sqlc.arg(now), sqlc.arg(now)
FROM generate_subscripts(sqlc.arg(quest_ids)::text[], 1) AS k
ON CONFLICT (character_id, quest_id) DO UPDATE
SET status = EXCLUDED.status, progress = EXCLUDED.progress, times_finished = EXCLUDED.times_finished, updated_at = EXCLUDED.updated_at
WHERE (character_quests.status, character_quests.progress, character_quests.times_finished) IS DISTINCT FROM (EXCLUDED.status, EXCLUDED.progress, EXCLUDED.times_finished);

-- name: DropOtherQuests :exec
DELETE FROM character_quests WHERE character_id = sqlc.arg(character_id) AND quest_id <> ALL(sqlc.arg(quest_ids)::text[]);

-- name: ListSkills :many
SELECT skill_id, points FROM character_skills WHERE character_id = $1 ORDER BY skill_id;

-- name: SaveSkills :exec
-- Points are never taken back, so a skill not listed keeps its row; a row already as given is left alone.
INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at)
SELECT sqlc.arg(character_id), (sqlc.arg(skill_ids)::text[])[k], (sqlc.arg(points)::int[])[k], sqlc.arg(now), sqlc.arg(now)
FROM generate_subscripts(sqlc.arg(skill_ids)::text[], 1) AS k
ON CONFLICT (character_id, skill_id) DO UPDATE
SET points = EXCLUDED.points, updated_at = EXCLUDED.updated_at
WHERE character_skills.points IS DISTINCT FROM EXCLUDED.points;
