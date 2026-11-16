-- +goose Up
-- When each skill's cooldown and buff ends, as the world's last checkpoint left them, so a fresh join counts them on. Each checkpoint writes the ones that changed and drops the ones it no longer lists.
CREATE TABLE character_skill_timers (
  character_id uuid NOT NULL REFERENCES characters (id),
  skill_id     text NOT NULL,                                -- from game data
  kind         text NOT NULL CHECK (kind IN ('cooldown', 'buff')),
  until        timestamptz NOT NULL,
  points       int NOT NULL CHECK (points >= 0),             -- a buff's points, which give its bonus; 0 for a cooldown
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (character_id, skill_id, kind),
  CHECK (kind = 'cooldown' OR points >= 1)
);

-- +goose Down
DROP TABLE character_skill_timers;
