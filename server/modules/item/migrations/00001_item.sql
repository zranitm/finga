-- +goose Up
-- The module stamps every table's created_at and updated_at from its own clock: with no default or trigger, a write that leaves one out fails on NOT NULL.

-- The outbox (pkg/outbox) and idempotency keys (pkg/idempotency), in the shape those packages write, owned by the item module, their only user.
CREATE TABLE outbox (
  id           bigserial PRIMARY KEY,
  event_id     uuid NOT NULL UNIQUE,
  topic        text NOT NULL,
  key          text NOT NULL,
  type         text NOT NULL,
  content_type text NOT NULL,                  -- application/json now, protobuf in Stage 2
  payload      bytea NOT NULL,
  occurred_at  timestamptz NOT NULL,
  published_at timestamptz,                    -- set by the relay (Stage 2)
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL
);
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
-- Rows are deleted oldest first: published ones a while after publishing, and in Stage 1 unpublished ones past their TTL.
CREATE INDEX outbox_published_by_age ON outbox (published_at) WHERE published_at IS NOT NULL;
CREATE INDEX outbox_unpublished_by_age ON outbox (created_at) WHERE published_at IS NULL;

CREATE TABLE idempotency_keys (
  owner        text NOT NULL,                  -- account ID
  scope        text NOT NULL,                  -- command type
  key          text NOT NULL,
  request_hash bytea NOT NULL,
  response     bytea NOT NULL,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (owner, scope, key)
);
-- Keys past the retry window are deleted oldest first.
CREATE INDEX idempotency_keys_by_age ON idempotency_keys (created_at);

-- owner_id has no foreign key, as modules refer to each other by ID only. A worn item is in the equipped location with the equip slot it fills (items.yaml's equip_slots), and in no inventory slot.
CREATE TABLE items (
  id          uuid PRIMARY KEY,
  template_id text NOT NULL,                   -- from game data
  owner_id    uuid NOT NULL,                   -- character
  location    text NOT NULL,                   -- inventory | equipped | listed | destroyed
  tab         text,                            -- equip | use | etc, only in the inventory
  slot        int,
  quantity    int NOT NULL CHECK (quantity >= 0),
  updated_at  timestamptz NOT NULL,
  created_at  timestamptz NOT NULL,
  equip_slot  text,
  CHECK ((location = 'inventory') = (tab IS NOT NULL AND slot IS NOT NULL)),
  CHECK ((location = 'destroyed') = (quantity = 0)),
  CHECK ((location = 'equipped') = (equip_slot IS NOT NULL)),
  -- One item per equip slot, checked at commit, so a swap moves the new item in before the worn one leaves.
  CONSTRAINT one_item_per_equip_slot EXCLUDE USING btree (owner_id WITH =, equip_slot WITH =) WHERE (location = 'equipped') DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX one_item_per_slot ON items (owner_id, tab, slot) WHERE location = 'inventory';
-- A character's items are its inventory and what it wears, read and locked together.
CREATE INDEX items_by_owner ON items (owner_id) WHERE location IN ('inventory', 'equipped');

-- The append-only history of every item; occurred_at is the event's time, which consumers read. A quest's hand-in takes items (kind handed_in) and gives its rewards, each event with cause_type quest naming the quest.
CREATE TABLE item_events (
  id             bigserial PRIMARY KEY,
  item_id        uuid NOT NULL REFERENCES items(id),
  kind           text NOT NULL,   -- created | stacked | split | moved | listed | unlisted | sold | discarded | handed_in | ...
  quantity_delta int NOT NULL,    -- change to this item's quantity
  from_owner     uuid,
  to_owner       uuid,
  cause_type     text NOT NULL,   -- drop | market | player | starter | quest | ...
  cause_id       uuid,            -- drop, listing, request or hand-in ID
  occurred_at    timestamptz NOT NULL,
  created_at     timestamptz NOT NULL,
  updated_at     timestamptz NOT NULL,
  quest_id       text,
  CHECK ((cause_type = 'quest') = (quest_id IS NOT NULL))
);
CREATE INDEX item_events_by_item ON item_events (item_id, id);

-- One row per character given its starter kit, kept for good: GrantStarter runs on every join, so it cannot rest on an idempotency key a cleanup deletes.
CREATE TABLE starter_grants (
  character_id uuid PRIMARY KEY,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL
);

-- An inventory's revision counts its changes, so a client holding two views of a slot keeps the newer; no row is revision 0.
CREATE TABLE inventory_revisions (
  owner_id   uuid PRIMARY KEY,   -- character
  revision   bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

-- What each equipment item rolled when it dropped, one column per stat (rates in tenths of a percent); insert-only, as a roll is part of the item for its life.
CREATE TABLE item_rolls (
  item_id          uuid PRIMARY KEY REFERENCES items(id),
  physical_attack  int NOT NULL,
  physical_defense int NOT NULL,
  str              int NOT NULL,
  dex              int NOT NULL,
  "int"            int NOT NULL,
  agi              int NOT NULL,
  vit              int NOT NULL,
  accuracy         int NOT NULL,
  evasion          int NOT NULL,
  crit_rate        int NOT NULL,
  crit_damage      int NOT NULL,
  max_hp           int NOT NULL,
  max_mp           int NOT NULL,
  created_at       timestamptz NOT NULL,
  updated_at       timestamptz NOT NULL,
  magic_attack     int NOT NULL DEFAULT 0,
  magic_defense    int NOT NULL DEFAULT 0,
  penetration      int NOT NULL DEFAULT 0,
  hp_regen         int NOT NULL DEFAULT 0,
  mp_regen         int NOT NULL DEFAULT 0,
  attack_speed     int NOT NULL DEFAULT 0,
  cooldown_speed   int NOT NULL DEFAULT 0,
  move_speed       int NOT NULL DEFAULT 0
);

-- One row per completion of a quest handed in, kept for good: a world that missed the reply finishes the quest from it, however late. Its id is the cause id of the hand-in's events.
CREATE TABLE quest_hand_ins (
  character_id uuid    NOT NULL,
  quest_id     text    NOT NULL,
  completion   integer NOT NULL CHECK (completion > 0),
  id           uuid    NOT NULL UNIQUE,
  request_hash bytea   NOT NULL,
  result       jsonb   NOT NULL,
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (character_id, quest_id, completion)
);

-- +goose Down
DROP TABLE quest_hand_ins;
DROP TABLE item_rolls;
DROP TABLE inventory_revisions;
DROP TABLE starter_grants;
DROP TABLE item_events;
DROP TABLE items;
DROP TABLE idempotency_keys;
DROP TABLE outbox;
