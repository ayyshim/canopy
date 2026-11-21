-- +goose Up
-- A goal finishes only after its person approves a handover brief written by the manager.
ALTER TABLE tasks DROP CONSTRAINT tasks_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_kind_check CHECK (kind IN ('plan', 'work', 'owner_change', 'triage', 'handover'));
ALTER TABLE goals ADD COLUMN handover_approved_by uuid REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE goals ADD COLUMN handover_approved_at timestamptz;

-- +goose Down
ALTER TABLE goals DROP COLUMN handover_approved_at;
ALTER TABLE goals DROP COLUMN handover_approved_by;
UPDATE tasks SET kind = 'work' WHERE kind = 'handover';
ALTER TABLE tasks DROP CONSTRAINT tasks_kind_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_kind_check CHECK (kind IN ('plan', 'work', 'owner_change', 'triage'));
