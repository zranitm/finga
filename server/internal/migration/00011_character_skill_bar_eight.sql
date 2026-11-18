-- +goose Up
-- Eight skill keys, Q W E R A S D F (B6): each bar keeps its six skills in order and gains two empty keys.
ALTER TABLE characters DROP CONSTRAINT skill_bar_six_keys;
UPDATE characters SET skill_bar = skill_bar || '{"",""}'::text[];
ALTER TABLE characters
  ALTER COLUMN skill_bar SET DEFAULT '{"","","","","","","",""}',
  ADD CONSTRAINT skill_bar_eight_keys CHECK (cardinality(skill_bar) = 8);

-- +goose Down
ALTER TABLE characters DROP CONSTRAINT skill_bar_eight_keys;
UPDATE characters SET skill_bar = skill_bar[1:6];
ALTER TABLE characters
  ALTER COLUMN skill_bar SET DEFAULT '{"","","","","",""}',
  ADD CONSTRAINT skill_bar_six_keys CHECK (cardinality(skill_bar) = 6);
