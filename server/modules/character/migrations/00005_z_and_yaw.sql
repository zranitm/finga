-- +goose Up
-- 3D: a character's place gains z, metres north, and the yaw it faces, radians from north toward east. A row from before gets 0 for both.
ALTER TABLE characters
  ADD COLUMN pos_z real NOT NULL DEFAULT 0,
  ADD COLUMN yaw real NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE characters DROP COLUMN yaw, DROP COLUMN pos_z;
