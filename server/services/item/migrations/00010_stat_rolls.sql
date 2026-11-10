-- +goose Up
-- Rolls name both attacks and both defenses, and may give the eight derived stats the stat system adds; a roll from before gives none of them.
ALTER TABLE item_rolls RENAME COLUMN attack TO physical_attack;
ALTER TABLE item_rolls RENAME COLUMN defense TO physical_defense;
ALTER TABLE item_rolls
  ADD COLUMN magic_attack   int NOT NULL DEFAULT 0,
  ADD COLUMN magic_defense  int NOT NULL DEFAULT 0,
  ADD COLUMN penetration    int NOT NULL DEFAULT 0,
  ADD COLUMN hp_regen       int NOT NULL DEFAULT 0,
  ADD COLUMN mp_regen       int NOT NULL DEFAULT 0,
  ADD COLUMN attack_speed   int NOT NULL DEFAULT 0,
  ADD COLUMN cooldown_speed int NOT NULL DEFAULT 0,
  ADD COLUMN move_speed     int NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE item_rolls
  DROP COLUMN magic_attack,
  DROP COLUMN magic_defense,
  DROP COLUMN penetration,
  DROP COLUMN hp_regen,
  DROP COLUMN mp_regen,
  DROP COLUMN attack_speed,
  DROP COLUMN cooldown_speed,
  DROP COLUMN move_speed;
ALTER TABLE item_rolls RENAME COLUMN physical_defense TO defense;
ALTER TABLE item_rolls RENAME COLUMN physical_attack TO attack;
