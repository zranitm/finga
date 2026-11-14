-- +goose Up
-- Energy in place of MP: it pays for skills, is kept between maps and lost on death, and a new character starts with none. Its most is game data, which the world holds it to.
ALTER TABLE characters
  DROP COLUMN mp,
  ADD COLUMN energy smallint NOT NULL DEFAULT 0,
  ADD CONSTRAINT energy_not_negative CHECK (energy >= 0);

-- +goose Down
ALTER TABLE characters DROP CONSTRAINT energy_not_negative, DROP COLUMN energy, ADD COLUMN mp int NOT NULL DEFAULT 0;
