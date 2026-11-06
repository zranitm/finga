-- +goose Up
-- The service stamps created_at and updated_at from its own clock: with no default or trigger left, a write that leaves one out fails on NOT NULL.
DROP TRIGGER set_updated_at ON items;
DROP TRIGGER set_updated_at ON item_events;
DROP TRIGGER set_updated_at ON starter_grants;
DROP TRIGGER set_updated_at ON outbox;
DROP TRIGGER set_updated_at ON idempotency_keys;
ALTER TABLE items ALTER COLUMN created_at DROP DEFAULT, ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE item_events ALTER COLUMN created_at DROP DEFAULT, ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE starter_grants ALTER COLUMN created_at DROP DEFAULT, ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE outbox ALTER COLUMN created_at DROP DEFAULT, ALTER COLUMN updated_at DROP DEFAULT;
ALTER TABLE idempotency_keys ALTER COLUMN created_at DROP DEFAULT, ALTER COLUMN updated_at DROP DEFAULT;

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
ALTER TABLE idempotency_keys ALTER COLUMN created_at SET DEFAULT now(), ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE outbox ALTER COLUMN created_at SET DEFAULT now(), ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE starter_grants ALTER COLUMN created_at SET DEFAULT now(), ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE item_events ALTER COLUMN created_at SET DEFAULT now(), ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE items ALTER COLUMN created_at SET DEFAULT now(), ALTER COLUMN updated_at SET DEFAULT now();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON idempotency_keys FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON outbox FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON starter_grants FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON item_events FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_updated_at BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION set_updated_at();
