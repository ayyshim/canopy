-- +goose Up
-- Triage tasks are requests people give a manager; the manager hands them to staff.
ALTER TABLE tasks DROP CONSTRAINT tasks_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_kind_check CHECK (kind IN ('plan', 'work', 'owner_change', 'triage'));

-- +goose Down
UPDATE tasks SET kind = 'work' WHERE kind = 'triage';
ALTER TABLE tasks DROP CONSTRAINT tasks_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_kind_check CHECK (kind IN ('plan', 'work', 'owner_change'));
