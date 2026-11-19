-- +goose Up
-- Outfits (docs/tech/outfits.md section 4): each account's wardrobe and each character's look, kept by the item module. account_id, character_id and item_id have no foreign key, as modules refer to each other by ID only. The module stamps the times.

-- The wardrobe: each piece an account has collected, and what gave it. A piece added from an item names that item, which has a collected event for it.
CREATE TABLE account_pieces (
  account_id uuid NOT NULL,
  piece_id   text NOT NULL,                    -- a piece in outfits.yaml
  cause_type text NOT NULL,                    -- item, rank, quest or feat
  cause_ref  text,                             -- the rank, quest or feat; null for an item
  item_id    uuid,                             -- the item added, for an item alone
  added_at   timestamptz NOT NULL,
  PRIMARY KEY (account_id, piece_id),
  CONSTRAINT account_pieces_cause CHECK (cause_type IN ('item', 'rank', 'quest', 'feat')),
  CONSTRAINT account_pieces_item CHECK ((cause_type = 'item') = (item_id IS NOT NULL) AND (cause_type = 'item') = (cause_ref IS NULL))
);

-- A character's look: a piece id in each slot it doesn't leave to its class, null keeping the class's own. No row is the class's own in every slot.
CREATE TABLE character_looks (
  character_id  uuid PRIMARY KEY,
  outfit        text,
  headwear      text,
  face          text,
  weapon        text,
  wings         text,                          -- null: none, the class's own
  hide_headwear boolean NOT NULL,
  revision      bigint NOT NULL CHECK (revision > 0), -- each change one more, so the world applies only a newer look
  created_at    timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL
);

-- +goose Down
DROP TABLE character_looks;
DROP TABLE account_pieces;
