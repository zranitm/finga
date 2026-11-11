-- +goose Up
-- Skill points: those unspent, beside the stat points, and those invested in each skill. A skill's stage is never stored: it comes from its points and the game data.
ALTER TABLE characters
  ADD COLUMN unspent_skill_points int NOT NULL DEFAULT 0,
  ADD CONSTRAINT skill_points_not_negative CHECK (unspent_skill_points >= 0);
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
ALTER TABLE characters DROP CONSTRAINT skill_points_not_negative, DROP COLUMN unspent_skill_points;
