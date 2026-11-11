-- +goose Up
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

-- +goose Down
DROP TABLE character_quests;
