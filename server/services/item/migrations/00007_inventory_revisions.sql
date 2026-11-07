-- +goose Up
-- An inventory's revision counts its changes, so a client holding two views of a slot keeps the newer; no row is revision 0.
CREATE TABLE inventory_revisions (
  owner_id   uuid PRIMARY KEY,   -- character
  revision   bigint NOT NULL CHECK (revision > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE inventory_revisions;
