-- name: LockOwner :exec
-- Orders one owner's inventory changes: a row lock cannot cover the empty slot two of them would pick.
SELECT pg_advisory_xact_lock(hashtextextended('item.inventory:' || sqlc.arg(owner_id)::text, 0));

-- name: LockInventory :many
-- Every row of the owner's inventory and of what it wears, locked in ID order so concurrent actions on one inventory never deadlock.
SELECT * FROM items
WHERE owner_id = $1 AND location IN ('inventory', 'equipped')
ORDER BY id
FOR UPDATE;

-- name: LockOwnedItem :one
SELECT * FROM items WHERE id = $1 AND owner_id = $2 FOR UPDATE;

-- name: ListInventory :many
-- The inventory by tab and slot, then what is worn by equip slot.
SELECT * FROM items
WHERE owner_id = $1 AND location IN ('inventory', 'equipped')
ORDER BY tab NULLS LAST, slot, equip_slot;

-- name: InsertItem :exec
-- A new item goes in an inventory slot, or straight into an equip slot.
INSERT INTO items (id, template_id, owner_id, location, tab, slot, equip_slot, quantity, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(template_id), sqlc.arg(owner_id), sqlc.arg(location), sqlc.narg(tab), sqlc.narg(slot), sqlc.narg(equip_slot), sqlc.arg(quantity), sqlc.arg(now), sqlc.arg(now));

-- name: InsertRoll :exec
INSERT INTO item_rolls (item_id, str, dex, "int", agi, vit, physical_attack, magic_attack, physical_defense, magic_defense, max_hp, max_mp, accuracy, evasion,
  crit_rate, crit_damage, penetration, hp_regen, mp_regen, attack_speed, cooldown_speed, move_speed, created_at, updated_at)
VALUES (sqlc.arg(item_id), sqlc.arg(str), sqlc.arg(dex), sqlc.arg(int), sqlc.arg(agi), sqlc.arg(vit),
  sqlc.arg(physical_attack), sqlc.arg(magic_attack), sqlc.arg(physical_defense), sqlc.arg(magic_defense), sqlc.arg(max_hp), sqlc.arg(max_mp), sqlc.arg(accuracy), sqlc.arg(evasion),
  sqlc.arg(crit_rate), sqlc.arg(crit_damage), sqlc.arg(penetration), sqlc.arg(hp_regen), sqlc.arg(mp_regen), sqlc.arg(attack_speed), sqlc.arg(cooldown_speed), sqlc.arg(move_speed),
  sqlc.arg(now), sqlc.arg(now));

-- name: ListRolls :many
-- The rolls of the items read beside them; a roll never changes, so it needs no lock.
SELECT * FROM item_rolls WHERE item_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: SetQuantity :execrows
UPDATE items SET quantity = sqlc.arg(quantity), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: SetSlot :execrows
-- Puts an item in an inventory slot, taking it off if it was worn.
UPDATE items SET location = 'inventory', tab = sqlc.arg(tab), slot = sqlc.arg(slot), equip_slot = NULL, updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: SetEquipped :execrows
UPDATE items SET location = 'equipped', equip_slot = sqlc.arg(equip_slot), tab = NULL, slot = NULL, updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: DestroyItem :execrows
UPDATE items SET location = 'destroyed', quantity = 0, tab = NULL, slot = NULL, updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: InsertItemEvent :exec
INSERT INTO item_events (item_id, kind, quantity_delta, from_owner, to_owner, cause_type, cause_id, quest_id, occurred_at, created_at, updated_at)
VALUES (sqlc.arg(item_id), sqlc.arg(kind), sqlc.arg(quantity_delta), sqlc.arg(from_owner), sqlc.arg(to_owner), sqlc.arg(cause_type), sqlc.arg(cause_id), sqlc.narg(quest_id), sqlc.arg(occurred_at), sqlc.arg(now), sqlc.arg(now));

-- name: RecordStarterGrant :execrows
-- Zero rows: the character already has its kit.
INSERT INTO starter_grants (character_id, created_at, updated_at) VALUES (sqlc.arg(character_id), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT DO NOTHING;

-- name: NextRevision :one
-- The owner's next revision, 1 first; the row's lock holds back a later change's revision until this one commits.
INSERT INTO inventory_revisions (owner_id, revision, created_at, updated_at) VALUES (sqlc.arg(owner_id), 1, sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (owner_id) DO UPDATE SET revision = inventory_revisions.revision + 1, updated_at = sqlc.arg(now)
RETURNING revision;

-- name: InventoryRevision :one
SELECT coalesce((SELECT revision FROM inventory_revisions WHERE owner_id = $1), 0)::bigint AS revision;
