-- +goose Up
-- PvP (docs/tech/pvp.md section 10): ratings and match history. character_id has no foreign key, as modules refer to each other by ID only. The module stamps created_at and updated_at.

-- One rating per character and rated mode. shown is written with mu and sigma, as pvp.yaml's shown rule computes it, so the leaderboard reads it by index.
CREATE TABLE pvp_ratings (
  character_id uuid NOT NULL,
  mode         text NOT NULL,                  -- a rated mode in pvp.yaml
  mu           double precision NOT NULL,
  sigma        double precision NOT NULL,
  shown        integer NOT NULL,
  matches      integer NOT NULL,
  wins         integer NOT NULL,
  losses       integer NOT NULL,
  draws        integer NOT NULL,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (character_id, mode),
  CONSTRAINT pvp_ratings_counts CHECK (matches >= 0 AND wins >= 0 AND losses >= 0 AND draws >= 0 AND wins + losses + draws = matches),
  CONSTRAINT pvp_ratings_sigma CHECK (sigma > 0)
);
CREATE INDEX pvp_ratings_board ON pvp_ratings (mode, shown DESC);

-- A match once it has an outcome: its id is the key, so a result told twice is stored once. winner is 1 or 2, 0 for a draw.
CREATE TABLE pvp_matches (
  id         uuid PRIMARY KEY,
  mode       text NOT NULL,
  started_at timestamptz NOT NULL,
  ended_at   timestamptz NOT NULL,
  winner     smallint NOT NULL CHECK (winner BETWEEN 0 AND 2),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

-- Each fighter of a match: a character, or a bot with its tier, and its rating before and after; a bot's is its tier's, unchanged.
CREATE TABLE pvp_match_fighters (
  match_id     uuid NOT NULL REFERENCES pvp_matches (id),
  side         smallint NOT NULL CHECK (side BETWEEN 1 AND 2),
  slot         smallint NOT NULL,
  character_id uuid,                           -- null for a bot
  bot_tier     text,                           -- a bot's
  left_match   boolean NOT NULL,
  downs        integer NOT NULL,
  mu_before    double precision NOT NULL,
  sigma_before double precision NOT NULL,
  mu_after     double precision NOT NULL,
  sigma_after  double precision NOT NULL,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (match_id, side, slot),
  CONSTRAINT pvp_match_fighters_one_kind CHECK ((character_id IS NULL) = (bot_tier IS NOT NULL))
);
CREATE INDEX pvp_match_fighters_by_character ON pvp_match_fighters (character_id, created_at DESC) WHERE character_id IS NOT NULL;

-- +goose Down
DROP TABLE pvp_match_fighters;
DROP TABLE pvp_matches;
DROP TABLE pvp_ratings;
