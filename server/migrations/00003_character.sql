-- +goose Up
-- The character module's table (data model, page 2). account_id has no
-- foreign key: modules refer to each other by ID only.
CREATE TABLE characters (
  id             uuid PRIMARY KEY,
  account_id     uuid NOT NULL,
  name           citext NOT NULL UNIQUE,
  class          text NOT NULL,                -- warrior | bowman | thief | magician
  level          int NOT NULL CHECK (level >= 1),
  exp            bigint NOT NULL CHECK (exp >= 0),  -- EXP within the current level
  strength       int NOT NULL,
  dexterity      int NOT NULL,
  intelligence   int NOT NULL,
  agility        int NOT NULL,
  vitality       int NOT NULL,
  unspent_points int NOT NULL DEFAULT 0,
  hp             int NOT NULL,
  mp             int NOT NULL,
  map_id         text NOT NULL,
  pos_x          real NOT NULL,
  pos_y          real NOT NULL,
  updated_at     timestamptz NOT NULL
);
CREATE INDEX characters_by_account ON characters (account_id, updated_at);

-- +goose Down
DROP TABLE characters;
