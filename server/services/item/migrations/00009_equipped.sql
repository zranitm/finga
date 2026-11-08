-- +goose Up
-- A worn item is in the equipped location with the equip slot it fills (items.yaml's equip_slots), and in no inventory slot.
ALTER TABLE items
  ADD COLUMN equip_slot text,
  ADD CHECK ((location = 'equipped') = (equip_slot IS NOT NULL));

-- One item per equip slot, checked at commit, so a swap moves the new item in before the worn one leaves.
ALTER TABLE items ADD CONSTRAINT one_item_per_equip_slot
  EXCLUDE USING btree (owner_id WITH =, equip_slot WITH =) WHERE (location = 'equipped')
  DEFERRABLE INITIALLY DEFERRED;

-- A character's items are its inventory and what it wears, read and locked together.
DROP INDEX items_by_owner;
CREATE INDEX items_by_owner ON items (owner_id) WHERE location IN ('inventory', 'equipped');

-- +goose Down
DROP INDEX items_by_owner;
CREATE INDEX items_by_owner ON items (owner_id) WHERE location = 'inventory';
ALTER TABLE items DROP CONSTRAINT one_item_per_equip_slot;
ALTER TABLE items DROP COLUMN equip_slot;
