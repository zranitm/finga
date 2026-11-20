-- +goose Up
-- PvP (docs/tech/pvp.md): the arena builds (section 6), and the placed flag the leaderboard reads (section 10). character_id has no foreign key, as modules refer to each other by ID only.

-- A character's arena build once they save one: eight keys, '' for an empty one, and the trait chosen for skills on it. revision counts the saves, so a match keeps the newest it's sent, in whatever order the sends land.
CREATE TABLE pvp_builds (
  character_id uuid PRIMARY KEY,
  skill_bar    text[] NOT NULL CHECK (cardinality(skill_bar) = 8),
  traits       jsonb NOT NULL,
  revision     integer NOT NULL CHECK (revision > 0),
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL
);

-- placed is written with shown, by pvp.yaml's placement: the board lists placed ratings alone, read in order from this index, and a place counts those above by it.
ALTER TABLE pvp_ratings ADD COLUMN placed boolean NOT NULL DEFAULT false;
UPDATE pvp_ratings SET placed = matches >= 5; -- pvp.yaml's placement as this was written
ALTER TABLE pvp_ratings ALTER COLUMN placed DROP DEFAULT;
DROP INDEX pvp_ratings_board;
CREATE INDEX pvp_ratings_board ON pvp_ratings (mode, shown DESC, character_id) WHERE placed;

-- +goose Down
DROP INDEX pvp_ratings_board;
CREATE INDEX pvp_ratings_board ON pvp_ratings (mode, shown DESC);
ALTER TABLE pvp_ratings DROP COLUMN placed;
DROP TABLE pvp_builds;
