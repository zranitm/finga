-- +goose Up
-- Energy in place of MP: it pays for skills, is kept between maps and lost on death, and a new character starts with none.
ALTER TABLE characters
  DROP COLUMN mp,
  ADD COLUMN energy smallint NOT NULL DEFAULT 0,
  ADD CONSTRAINT energy_range CHECK (energy BETWEEN 0 AND 10);

-- +goose Down
ALTER TABLE characters DROP CONSTRAINT energy_range, DROP COLUMN energy, ADD COLUMN mp int NOT NULL DEFAULT 0;
