-- +goose Up
-- Keys past the retry window are deleted oldest first, as pkg/idempotency's Schema indexes them.
CREATE INDEX idempotency_keys_by_age ON idempotency_keys (created_at);

-- +goose Down
DROP INDEX idempotency_keys_by_age;
