-- +goose Up
-- Case-insensitive text for character names, which the account module creates too; neither drops it, so rolling one back never breaks the other.
CREATE EXTENSION IF NOT EXISTS citext;

-- account_id has no foreign key, as modules refer to each other by ID only. The module stamps created_at and updated_at from its own clock: with no default or trigger, a write that leaves one out fails on NOT NULL.
-- The five *_points columns hold the points spent in each stat, not totals: the own stats are the class's starting stats, from game data, plus these.
CREATE TABLE characters (
  id                   uuid PRIMARY KEY,
  account_id           uuid NOT NULL,
  name                 citext NOT NULL UNIQUE,
  class                text NOT NULL,                     -- from game data
  level                int NOT NULL CHECK (level >= 1),
  exp                  bigint NOT NULL CHECK (exp >= 0),  -- EXP within the current level
  str_points           int NOT NULL,
  dex_points           int NOT NULL,
  int_points           int NOT NULL,
  agi_points           int NOT NULL,
  vit_points           int NOT NULL,
  unspent_points       int NOT NULL DEFAULT 0,
  hp                   int NOT NULL,
  mp                   int NOT NULL,
  map_id               text NOT NULL,
  pos_x                real NOT NULL,
  pos_y                real NOT NULL,
  updated_at           timestamptz NOT NULL,
  created_at           timestamptz NOT NULL,
  free_reset_used      boolean NOT NULL DEFAULT false,
  unspent_skill_points int NOT NULL DEFAULT 0,       -- beside the stat points; those invested are in character_skills
  CONSTRAINT points_not_negative CHECK (str_points >= 0 AND dex_points >= 0 AND int_points >= 0 AND agi_points >= 0 AND vit_points >= 0 AND unspent_points >= 0),
  CONSTRAINT skill_points_not_negative CHECK (unspent_skill_points >= 0)
);
-- Characters list oldest first: created_at, not updated_at, which moves with every checkpoint and would make no save HOT.
CREATE INDEX characters_by_account ON characters (account_id, created_at);

-- Each character's quests: active ones with a count per objective, and finished ones with how often. A repeatable quest active again keeps its count; an active quest never finished has no row once abandoned.
CREATE TABLE character_quests (
  character_id   uuid NOT NULL REFERENCES characters (id),
  quest_id       text NOT NULL,                -- from game data
  status         text NOT NULL CHECK (status IN ('active', 'finished')),
  progress       int[] NOT NULL,               -- one count per objective, in the quest's order; empty when finished
  times_finished int NOT NULL CHECK (times_finished >= 0),
  created_at     timestamptz NOT NULL,
  updated_at     timestamptz NOT NULL,
  PRIMARY KEY (character_id, quest_id),
  CHECK (status = 'active' OR (times_finished >= 1 AND cardinality(progress) = 0))
);

-- The points invested in each skill. A skill's stage is never stored: it comes from its points and the game data.
CREATE TABLE character_skills (
  character_id uuid NOT NULL REFERENCES characters (id),
  skill_id     text NOT NULL,              -- from game data
  points       int NOT NULL CHECK (points > 0),
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (character_id, skill_id)
);

-- +goose Down
DROP TABLE character_skills;
DROP TABLE character_quests;
DROP TABLE characters;
-- citext is left in place: the account module may use it.
