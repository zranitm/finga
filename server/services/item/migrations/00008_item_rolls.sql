-- +goose Up
-- What each equipment item rolled when it dropped, one column per stat (rates in tenths of a percent); insert-only, as a roll is part of the item for its life.
CREATE TABLE item_rolls (
  item_id     uuid PRIMARY KEY REFERENCES items(id),
  attack      int NOT NULL,
  defense     int NOT NULL,
  str         int NOT NULL,
  dex         int NOT NULL,
  "int"       int NOT NULL,
  agi         int NOT NULL,
  vit         int NOT NULL,
  accuracy    int NOT NULL,
  evasion     int NOT NULL,
  crit_rate   int NOT NULL,
  crit_damage int NOT NULL,
  max_hp      int NOT NULL,
  max_mp      int NOT NULL,
  created_at  timestamptz NOT NULL,
  updated_at  timestamptz NOT NULL
);

-- Equipment from before rolls gets its template's lowest roll, as starter equipment does; these are the only equipment templates items.yaml had.
INSERT INTO item_rolls (item_id, attack, defense, str, dex, "int", agi, vit, accuracy, evasion, crit_rate, crit_damage, max_hp, max_mp, created_at, updated_at)
SELECT i.id, lowest.attack, lowest.defense, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, i.created_at, i.created_at
FROM items i
JOIN (VALUES
  ('wooden_sword', 10, 0),
  ('short_bow', 9, 0),
  ('iron_sword', 16, 0),
  ('hunting_bow', 14, 0),
  ('leather_cap', 0, 3),
  ('leather_vest', 0, 5)
) AS lowest (template_id, attack, defense) USING (template_id);

-- +goose Down
DROP TABLE item_rolls;
