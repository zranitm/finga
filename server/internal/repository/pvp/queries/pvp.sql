-- name: Rating :one
SELECT mu, sigma, matches, wins, losses, draws FROM pvp_ratings WHERE character_id = $1 AND mode = $2;

-- name: RecordMatch :execrows
INSERT INTO pvp_matches (id, mode, started_at, ended_at, winner, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(mode), sqlc.arg(started_at), sqlc.arg(ended_at), sqlc.arg(winner), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (id) DO NOTHING;

-- name: LockRatings :many
SELECT character_id, mu, sigma, matches, wins, losses, draws FROM pvp_ratings
WHERE mode = sqlc.arg(mode) AND character_id = ANY(sqlc.arg(characters)::uuid[])
ORDER BY character_id
FOR UPDATE;

-- name: PutRating :exec
INSERT INTO pvp_ratings (character_id, mode, mu, sigma, shown, matches, wins, losses, draws, created_at, updated_at)
VALUES (sqlc.arg(character_id), sqlc.arg(mode), sqlc.arg(mu), sqlc.arg(sigma), sqlc.arg(shown), sqlc.arg(matches), sqlc.arg(wins), sqlc.arg(losses), sqlc.arg(draws), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (character_id, mode) DO UPDATE SET
  mu = EXCLUDED.mu, sigma = EXCLUDED.sigma, shown = EXCLUDED.shown, matches = EXCLUDED.matches,
  wins = EXCLUDED.wins, losses = EXCLUDED.losses, draws = EXCLUDED.draws, updated_at = EXCLUDED.updated_at;

-- name: RecordFighter :exec
INSERT INTO pvp_match_fighters (match_id, side, slot, character_id, bot_tier, left_match, downs, mu_before, sigma_before, mu_after, sigma_after, created_at, updated_at)
VALUES (sqlc.arg(match_id), sqlc.arg(side), sqlc.arg(slot), sqlc.narg(character_id), sqlc.narg(bot_tier), sqlc.arg(left_match), sqlc.arg(downs), sqlc.arg(mu_before), sqlc.arg(sigma_before), sqlc.arg(mu_after), sqlc.arg(sigma_after), sqlc.arg(now), sqlc.arg(now));
