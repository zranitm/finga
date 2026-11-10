-- +goose Up
-- The five stat columns hold the points spent in each stat, not totals: the own stats are the class's starting stats, from game data, plus these. The release starts from an empty database, so nothing is converted.
ALTER TABLE characters RENAME COLUMN strength TO str_points;
ALTER TABLE characters RENAME COLUMN dexterity TO dex_points;
ALTER TABLE characters RENAME COLUMN intelligence TO int_points;
ALTER TABLE characters RENAME COLUMN agility TO agi_points;
ALTER TABLE characters RENAME COLUMN vitality TO vit_points;
ALTER TABLE characters
  ADD COLUMN free_reset_used boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT points_not_negative CHECK (str_points >= 0 AND dex_points >= 0 AND int_points >= 0 AND agi_points >= 0 AND vit_points >= 0 AND unspent_points >= 0);

-- +goose Down
ALTER TABLE characters DROP CONSTRAINT points_not_negative, DROP COLUMN free_reset_used;
ALTER TABLE characters RENAME COLUMN vit_points TO vitality;
ALTER TABLE characters RENAME COLUMN agi_points TO agility;
ALTER TABLE characters RENAME COLUMN int_points TO intelligence;
ALTER TABLE characters RENAME COLUMN dex_points TO dexterity;
ALTER TABLE characters RENAME COLUMN str_points TO strength;
