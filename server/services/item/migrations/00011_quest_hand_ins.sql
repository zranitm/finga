-- +goose Up
-- A quest's hand-in takes items (kind handed_in) and gives its rewards, each event with cause_type quest naming the quest.
ALTER TABLE item_events
  ADD COLUMN quest_id text,
  ADD CHECK ((cause_type = 'quest') = (quest_id IS NOT NULL));

-- +goose Down
ALTER TABLE item_events DROP COLUMN quest_id;
