-- +goose Up
-- created_at and updated_at on every table, kept by the database.
-- The character and item services create set_updated_at() too; none drops it, so a rollback never breaks the others.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd

ALTER TABLE accounts
  ALTER COLUMN created_at SET DEFAULT now(),
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE accounts SET updated_at = created_at;

ALTER TABLE sessions
  ALTER COLUMN created_at SET DEFAULT now(),
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE sessions SET updated_at = GREATEST(created_at, rotated_at, revoked_at);

CREATE TRIGGER set_updated_at BEFORE UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON sessions FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER set_updated_at ON sessions;
DROP TRIGGER set_updated_at ON accounts;
ALTER TABLE sessions DROP COLUMN updated_at, ALTER COLUMN created_at DROP DEFAULT;
ALTER TABLE accounts DROP COLUMN updated_at, ALTER COLUMN created_at DROP DEFAULT;
-- set_updated_at() is left in place: the other services may use it.
