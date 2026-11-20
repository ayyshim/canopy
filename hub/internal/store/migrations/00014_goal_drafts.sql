-- +goose Up
ALTER TABLE goals DROP CONSTRAINT goals_status_check;
ALTER TABLE goals ADD CONSTRAINT goals_status_check CHECK (status IN ('draft', 'planning', 'active', 'ready', 'done'));

-- +goose Down
DELETE FROM goals WHERE status = 'draft';
ALTER TABLE goals DROP CONSTRAINT goals_status_check;
ALTER TABLE goals ADD CONSTRAINT goals_status_check CHECK (status IN ('planning', 'active', 'ready', 'done'));
