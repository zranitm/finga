-- +goose Up
-- Characters list oldest first; updated_at moves with every checkpoint, so it reordered the list and made no save HOT.
DROP INDEX characters_by_account;
CREATE INDEX characters_by_account ON characters (account_id, created_at);

-- +goose Down
DROP INDEX characters_by_account;
CREATE INDEX characters_by_account ON characters (account_id, updated_at);
