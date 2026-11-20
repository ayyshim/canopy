-- +goose Up
-- Isolated staff are independent staff only their creator can chat with.
ALTER TABLE members ADD COLUMN isolated boolean NOT NULL DEFAULT false;
ALTER TABLE members ADD COLUMN created_by uuid REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE members ADD CONSTRAINT members_isolated_independent
    CHECK (NOT isolated OR (kind = 'staff' AND reports_to IS NULL));

-- +goose Down
ALTER TABLE members DROP CONSTRAINT members_isolated_independent;
ALTER TABLE members DROP COLUMN created_by, DROP COLUMN isolated;
