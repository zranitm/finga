-- +goose Up
-- Outbox rows are deleted oldest first: published ones a while after publishing, and in Stage 1 unpublished ones past their TTL, as pkg/outbox's Schema indexes them.
CREATE INDEX outbox_published_by_age ON outbox (published_at) WHERE published_at IS NOT NULL;
CREATE INDEX outbox_unpublished_by_age ON outbox (created_at) WHERE published_at IS NULL;

-- +goose Down
DROP INDEX outbox_unpublished_by_age;
DROP INDEX outbox_published_by_age;
