-- name: LockOwner :exec
-- Orders one owner's inventory changes: a row lock cannot cover the empty slot two of them would pick.
SELECT pg_advisory_xact_lock(hashtextextended('item.inventory:' || sqlc.arg(owner_id)::text, 0));

-- name: LockInventory :many
-- Every inventory row of the owner, locked in ID order so concurrent actions on one inventory never deadlock.
SELECT * FROM items
WHERE owner_id = $1 AND location = 'inventory'
ORDER BY id
FOR UPDATE;

-- name: LockOwnedItem :one
SELECT * FROM items WHERE id = $1 AND owner_id = $2 FOR UPDATE;

-- name: ListInventory :many
SELECT * FROM items
WHERE owner_id = $1 AND location = 'inventory'
ORDER BY tab, slot;

-- name: InsertItem :exec
INSERT INTO items (id, template_id, owner_id, location, tab, slot, quantity, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(template_id), sqlc.arg(owner_id), 'inventory', sqlc.arg(tab), sqlc.arg(slot), sqlc.arg(quantity), sqlc.arg(now), sqlc.arg(now));

-- name: SetQuantity :execrows
UPDATE items SET quantity = sqlc.arg(quantity), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: SetSlot :execrows
UPDATE items SET tab = sqlc.arg(tab), slot = sqlc.arg(slot), updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: DestroyItem :execrows
UPDATE items SET location = 'destroyed', quantity = 0, tab = NULL, slot = NULL, updated_at = sqlc.arg(now) WHERE id = sqlc.arg(id);

-- name: InsertItemEvent :exec
INSERT INTO item_events (item_id, kind, quantity_delta, from_owner, to_owner, cause_type, cause_id, occurred_at, created_at, updated_at)
VALUES (sqlc.arg(item_id), sqlc.arg(kind), sqlc.arg(quantity_delta), sqlc.arg(from_owner), sqlc.arg(to_owner), sqlc.arg(cause_type), sqlc.arg(cause_id), sqlc.arg(occurred_at), sqlc.arg(now), sqlc.arg(now));

-- name: RecordStarterGrant :execrows
-- Zero rows: the character already has its kit.
INSERT INTO starter_grants (character_id, created_at, updated_at) VALUES (sqlc.arg(character_id), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT DO NOTHING;
