-- +goose Up
-- Refuses a database holding IDs minted before every ID was a UUIDv7, which no service parses, counting them per column.
-- +goose StatementBegin
DO $$
DECLARE
  col record;
  n bigint;
  bad text[] := '{}';
BEGIN
  FOR col IN
    SELECT c.relname AS tbl, a.attname AS name
    FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid
    WHERE c.relnamespace = to_regnamespace(current_schema()) AND c.relkind = 'r'
      AND a.atttypid = 'uuid'::regtype AND a.attnum > 0 AND NOT a.attisdropped
    ORDER BY c.relname, a.attnum
  LOOP
    EXECUTE format('SELECT count(*) FROM %2$I WHERE %1$I IS NOT NULL AND uuid_extract_version(%1$I) IS DISTINCT FROM 7', col.name, col.tbl) INTO n;
    IF n > 0 THEN
      bad := bad || format('%s.%s %s %s', col.tbl, col.name, n, CASE WHEN n = 1 THEN 'row' ELSE 'rows' END);
    END IF;
  END LOOP;
  IF cardinality(bad) > 0 THEN
    RAISE EXCEPTION '%', array_to_string(bad, ', ') USING ERRCODE = 'UUID7';
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;
