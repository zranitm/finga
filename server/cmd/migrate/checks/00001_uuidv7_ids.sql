-- +goose Up
-- Refuses a database holding an ID minted before every ID was a UUIDv7, which no service parses.
-- +goose StatementBegin
DO $$
DECLARE
  col record;
  bad text;
BEGIN
  FOR col IN
    SELECT c.relname AS tbl, a.attname AS name
    FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid
    WHERE c.relnamespace = to_regnamespace(current_schema()) AND c.relkind = 'r'
      AND a.atttypid = 'uuid'::regtype AND a.attnum > 0 AND NOT a.attisdropped
    ORDER BY c.relname, a.attnum
  LOOP
    EXECUTE format('SELECT %1$I::text FROM %2$I WHERE %1$I IS NOT NULL AND uuid_extract_version(%1$I) IS DISTINCT FROM 7 LIMIT 1', col.name, col.tbl) INTO bad;
    IF bad IS NOT NULL THEN
      RAISE EXCEPTION '%.% holds %', col.tbl, col.name, bad USING ERRCODE = 'UUID7';
    END IF;
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
