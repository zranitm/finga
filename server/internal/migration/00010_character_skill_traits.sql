-- +goose Up
-- Each skill's chosen trait, '' its default; a buff running keeps the trait it was used with, across maps and checkpoints.
ALTER TABLE character_skills ADD COLUMN trait text NOT NULL DEFAULT '';
ALTER TABLE character_skill_timers ADD COLUMN trait text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE character_skill_timers DROP COLUMN trait;
ALTER TABLE character_skills DROP COLUMN trait;
