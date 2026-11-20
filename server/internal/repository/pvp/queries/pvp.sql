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
INSERT INTO pvp_ratings (character_id, mode, mu, sigma, shown, placed, matches, wins, losses, draws, created_at, updated_at)
VALUES (sqlc.arg(character_id), sqlc.arg(mode), sqlc.arg(mu), sqlc.arg(sigma), sqlc.arg(shown), sqlc.arg(placed), sqlc.arg(matches), sqlc.arg(wins), sqlc.arg(losses), sqlc.arg(draws), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (character_id, mode) DO UPDATE SET
  mu = EXCLUDED.mu, sigma = EXCLUDED.sigma, shown = EXCLUDED.shown, placed = EXCLUDED.placed, matches = EXCLUDED.matches,
  wins = EXCLUDED.wins, losses = EXCLUDED.losses, draws = EXCLUDED.draws, updated_at = EXCLUDED.updated_at;

-- name: RecordFighter :exec
INSERT INTO pvp_match_fighters (match_id, side, slot, character_id, bot_tier, left_match, downs, mu_before, sigma_before, mu_after, sigma_after, created_at, updated_at)
VALUES (sqlc.arg(match_id), sqlc.arg(side), sqlc.arg(slot), sqlc.narg(character_id), sqlc.narg(bot_tier), sqlc.arg(left_match), sqlc.arg(downs), sqlc.arg(mu_before), sqlc.arg(sigma_before), sqlc.arg(mu_after), sqlc.arg(sigma_after), sqlc.arg(now), sqlc.arg(now));

-- name: StoredFighters :many
SELECT f.side, f.slot, f.character_id, f.mu_before, f.sigma_before, f.mu_after, f.sigma_after, r.matches, r.wins, r.losses, r.draws
FROM pvp_match_fighters f
JOIN pvp_ratings r ON r.character_id = f.character_id AND r.mode = sqlc.arg(mode)
WHERE f.match_id = sqlc.arg(match_id)
ORDER BY f.side, f.slot;

-- name: Ratings :many
SELECT mode, mu, sigma, matches, wins, losses, draws FROM pvp_ratings WHERE character_id = $1;

-- name: Board :many
-- The placed ratings of mode, highest first, read in order from pvp_ratings_board.
SELECT character_id, shown, matches, wins, losses, draws FROM pvp_ratings
WHERE mode = sqlc.arg(mode) AND placed
ORDER BY shown DESC, character_id
LIMIT sqlc.arg(most);

-- name: Above :one
SELECT count(*) FROM pvp_ratings WHERE mode = sqlc.arg(mode) AND placed AND shown > sqlc.arg(shown);

-- name: Build :one
SELECT skill_bar, traits, revision FROM pvp_builds WHERE character_id = $1;

-- name: PutBuild :one
INSERT INTO pvp_builds (character_id, skill_bar, traits, revision, created_at, updated_at)
VALUES (sqlc.arg(character_id), sqlc.arg(skill_bar), sqlc.arg(traits), 1, sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (character_id) DO UPDATE SET
  skill_bar = EXCLUDED.skill_bar, traits = EXCLUDED.traits, revision = pvp_builds.revision + 1, updated_at = EXCLUDED.updated_at
RETURNING revision;
