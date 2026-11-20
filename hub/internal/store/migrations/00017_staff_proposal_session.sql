-- +goose Up
-- The manager session that made the draft, so the decision can be sent back to it.
ALTER TABLE staff_proposals ADD COLUMN session_id uuid REFERENCES sessions (id) ON DELETE SET NULL;
-- Why the manager wants the change.
ALTER TABLE staff_proposals ADD COLUMN note text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE staff_proposals DROP COLUMN note, DROP COLUMN session_id;
