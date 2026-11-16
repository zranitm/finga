-- +goose Up
-- The game no longer has attack speed, so no roll gives it.
ALTER TABLE item_rolls DROP COLUMN attack_speed;

-- +goose Down
ALTER TABLE item_rolls ADD COLUMN attack_speed int NOT NULL DEFAULT 0;
