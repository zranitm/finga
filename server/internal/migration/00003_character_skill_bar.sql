-- +goose Up
-- The skill on each of keys 1 to 6, '' for none: an arrangement of the UI, which the world never reads.
ALTER TABLE characters
  ADD COLUMN skill_bar text[] NOT NULL DEFAULT '{"","","","","",""}',
  ADD CONSTRAINT skill_bar_six_keys CHECK (cardinality(skill_bar) = 6);

-- +goose Down
ALTER TABLE characters DROP CONSTRAINT skill_bar_six_keys, DROP COLUMN skill_bar;
