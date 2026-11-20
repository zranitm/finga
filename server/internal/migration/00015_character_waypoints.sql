-- +goose Up
-- The waypoints each character has found (large-regions.md section 8), which they may teleport to. A find is for good: each checkpoint adds those it lists that aren't here, and drops none.
CREATE TABLE character_waypoints (
  character_id uuid NOT NULL REFERENCES characters (id),
  waypoint_id  text NOT NULL,                  -- from a region's plan, its id unique across every region
  found_at     timestamptz NOT NULL,           -- when the world found it, not when a checkpoint saved it
  created_at   timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL,
  PRIMARY KEY (character_id, waypoint_id)
);

-- +goose Down
DROP TABLE character_waypoints;
