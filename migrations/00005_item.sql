-- +goose Up
-- The item module's tables (data model, page 2). owner_id has no foreign
-- key: modules refer to each other by ID only.
CREATE TABLE items (
  id          uuid PRIMARY KEY,
  template_id text NOT NULL,                   -- from game data
  owner_id    uuid NOT NULL,                   -- character
  location    text NOT NULL,                   -- inventory | listed | equipped (Stage 2) | destroyed
  tab         text,                            -- equip | use | etc, only in the inventory
  slot        int,
  quantity    int NOT NULL CHECK (quantity >= 0),
  updated_at  timestamptz NOT NULL,
  CHECK ((location = 'inventory') = (tab IS NOT NULL AND slot IS NOT NULL)),
  CHECK ((location = 'destroyed') = (quantity = 0))
);
CREATE UNIQUE INDEX one_item_per_slot
  ON items (owner_id, tab, slot) WHERE location = 'inventory';
CREATE INDEX items_by_owner ON items (owner_id) WHERE location = 'inventory';

-- append-only history of every item (D5)
CREATE TABLE item_events (
  id             bigserial PRIMARY KEY,
  item_id        uuid NOT NULL REFERENCES items(id),
  kind           text NOT NULL,   -- created | stacked | split | moved | listed | unlisted | sold | discarded
  quantity_delta int NOT NULL,    -- change to this item's quantity
  from_owner     uuid,
  to_owner       uuid,
  cause_type     text NOT NULL,   -- drop | market | player | starter
  cause_id       uuid,            -- drop, listing, or request ID
  occurred_at    timestamptz NOT NULL
);
CREATE INDEX item_events_by_item ON item_events (item_id, id);

-- +goose Down
DROP TABLE item_events;
DROP TABLE items;
