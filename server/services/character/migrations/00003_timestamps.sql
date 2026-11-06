-- +goose Up
-- created_at and updated_at on every table, kept by the database.
-- The account and item services create set_updated_at() too; none drops it, so a rollback never breaks the others.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd

-- An existing character's last save is the only time known for it.
ALTER TABLE characters
  ADD COLUMN created_at timestamptz NOT NULL DEFAULT now(),
  ALTER COLUMN updated_at SET DEFAULT now();
UPDATE characters SET created_at = updated_at;

CREATE TRIGGER set_updated_at BEFORE UPDATE ON characters FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER set_updated_at ON characters;
ALTER TABLE characters DROP COLUMN created_at, ALTER COLUMN updated_at DROP DEFAULT;
-- set_updated_at() is left in place: the other services may use it.
