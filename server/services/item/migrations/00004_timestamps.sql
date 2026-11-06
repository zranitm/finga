-- +goose Up
-- created_at and updated_at on every table, kept by the database.
-- The account and character services create set_updated_at() too; none drops it, so a rollback never breaks the others.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- An item was created at its first event; without one, its last change is the earliest time known.
ALTER TABLE items
  ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
  ALTER COLUMN updated_at SET DEFAULT now();
UPDATE items SET created_at = LEAST(updated_at, (SELECT min(occurred_at) FROM item_events WHERE item_id = items.id));

-- occurred_at stays beside them: it is the event's time, which consumers read.
ALTER TABLE item_events
  ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE item_events SET created_at = occurred_at, updated_at = occurred_at;

-- granted_at was the row's creation time.
ALTER TABLE starter_grants RENAME COLUMN granted_at TO created_at;
ALTER TABLE starter_grants
  ALTER COLUMN created_at SET DEFAULT now(),
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE starter_grants SET updated_at = created_at;

ALTER TABLE outbox
  ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE outbox SET created_at = occurred_at, updated_at = GREATEST(occurred_at, published_at);

ALTER TABLE idempotency_keys
  ALTER COLUMN created_at SET DEFAULT now(),
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE idempotency_keys SET updated_at = created_at;

CREATE TRIGGER set_updated_at BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON item_events FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON starter_grants FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON outbox FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON idempotency_keys FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER set_updated_at ON idempotency_keys;
DROP TRIGGER set_updated_at ON outbox;
DROP TRIGGER set_updated_at ON starter_grants;
DROP TRIGGER set_updated_at ON item_events;
DROP TRIGGER set_updated_at ON items;
ALTER TABLE idempotency_keys DROP COLUMN updated_at, ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE outbox DROP COLUMN created_at, DROP COLUMN updated_at;
ALTER TABLE starter_grants DROP COLUMN updated_at, ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE starter_grants RENAME COLUMN created_at TO granted_at;
ALTER TABLE item_events DROP COLUMN created_at, DROP COLUMN updated_at;
ALTER TABLE items DROP COLUMN created_at, ALTER COLUMN updated_at DROP DEFAULT;
-- set_updated_at() is left in place: the other services may use it.
