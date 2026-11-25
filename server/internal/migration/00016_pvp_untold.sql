-- +goose Up
-- A fighter away from their match at its end, held through a drop or gone, is told how it went at their next Welcome: untold until then, or until a gateway hands them the outcome live.
ALTER TABLE pvp_match_fighters ADD COLUMN untold boolean NOT NULL DEFAULT false;
CREATE INDEX pvp_match_fighters_untold ON pvp_match_fighters (character_id) WHERE untold;

-- +goose Down
DROP INDEX pvp_match_fighters_untold;
ALTER TABLE pvp_match_fighters DROP COLUMN untold;
