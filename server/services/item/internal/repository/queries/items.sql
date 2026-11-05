-- name: LockOwner :exec
-- Orders one owner's inventory changes: a row lock cannot cover the empty slot two of them would pick.
SELECT pg_advisory_xact_lock(hashtextextended('item.inventory:' || sqlc.arg(owner_id)::text, 0));

-- name: LockInventory :many
-- Every inventory row of the owner, locked in ID order so concurrent
-- actions on one inventory never deadlock.
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
INSERT INTO items (id, template_id, owner_id, location, tab, slot, quantity)
VALUES ($1, $2, $3, 'inventory', $4, $5, $6);

-- name: SetQuantity :execrows
UPDATE items SET quantity = $2 WHERE id = $1;

-- name: SetSlot :execrows
UPDATE items SET tab = $2, slot = $3 WHERE id = $1;

-- name: DestroyItem :execrows
UPDATE items SET location = 'destroyed', quantity = 0, tab = NULL, slot = NULL WHERE id = $1;

-- name: InsertItemEvent :exec
INSERT INTO item_events (item_id, kind, quantity_delta, from_owner, to_owner, cause_type, cause_id, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: RecordStarterGrant :execrows
-- Zero rows: the character already has its kit.
INSERT INTO starter_grants (character_id) VALUES ($1)
ON CONFLICT DO NOTHING;
