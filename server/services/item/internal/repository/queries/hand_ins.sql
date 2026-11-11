-- name: FindHandIn :one
SELECT id, request_hash, result FROM quest_hand_ins
WHERE character_id = sqlc.arg(character_id) AND quest_id = sqlc.arg(quest_id) AND completion = sqlc.arg(completion);

-- name: InsertHandIn :exec
INSERT INTO quest_hand_ins (character_id, quest_id, completion, id, request_hash, result, created_at, updated_at)
VALUES (sqlc.arg(character_id), sqlc.arg(quest_id), sqlc.arg(completion), sqlc.arg(id), sqlc.arg(request_hash), sqlc.arg(result), sqlc.arg(now), sqlc.arg(now));

-- name: ListHandIns :many
-- The character's hand-ins of the completions asked, each a quest at its index in quest_ids with the completion at that index in completions.
SELECT quest_id, completion, id, request_hash, result FROM quest_hand_ins
WHERE character_id = sqlc.arg(character_id)
  AND (quest_id, completion) IN (SELECT unnest(sqlc.arg(quest_ids)::text[]), unnest(sqlc.arg(completions)::integer[]))
ORDER BY quest_id, completion;
