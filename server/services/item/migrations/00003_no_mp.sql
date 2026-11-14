-- +goose Up
-- The game has energy in place of MP, so no roll gives max MP or MP regen.
ALTER TABLE item_rolls DROP COLUMN max_mp, DROP COLUMN mp_regen;

-- +goose Down
ALTER TABLE item_rolls ADD COLUMN max_mp int NOT NULL DEFAULT 0, ADD COLUMN mp_regen int NOT NULL DEFAULT 0;
