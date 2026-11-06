-- +goose Up
-- The service stamps created_at and updated_at from its own clock: with no default or trigger left, a write that leaves one out fails on NOT NULL.
DROP TRIGGER set_updated_at ON characters;
ALTER TABLE characters ALTER COLUMN created_at DROP DEFAULT, ALTER COLUMN updated_at DROP DEFAULT;

-- The services share set_updated_at(); the last one to stop using it drops it.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgfoid = to_regproc('set_updated_at')) THEN
    DROP FUNCTION IF EXISTS set_updated_at();
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END
$$;
-- +goose StatementEnd
ALTER TABLE characters ALTER COLUMN created_at SET DEFAULT now(), ALTER COLUMN updated_at SET DEFAULT now();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON characters FOR EACH ROW EXECUTE FUNCTION set_updated_at();
