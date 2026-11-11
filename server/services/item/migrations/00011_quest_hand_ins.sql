-- +goose Up
-- A quest's hand-in takes items (kind handed_in) and gives its rewards, each event with cause_type quest naming the quest.
ALTER TABLE item_events
  ADD COLUMN quest_id text,
  ADD CHECK ((cause_type = 'quest') = (quest_id IS NOT NULL));

-- One row per completion of a quest handed in, kept for good: a world that missed the reply finishes the quest from it, however late. Its id is the cause id of the hand-in's events.
CREATE TABLE quest_hand_ins (
  character_id uuid    NOT NULL,
  quest_id     text    NOT NULL,
  completion   integer NOT NULL CHECK (completion > 0),
  id           uuid    NOT NULL UNIQUE,
  request_hash bytea   NOT NULL,
  result       jsonb   NOT NULL,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (character_id, quest_id, completion)
);

-- +goose Down
DROP TABLE quest_hand_ins;
ALTER TABLE item_events DROP COLUMN quest_id;
