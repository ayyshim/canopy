-- +goose Up
ALTER TABLE organizations ADD COLUMN logo_key text;
ALTER TABLE workspaces ADD COLUMN logo_key text;

-- +goose Down
ALTER TABLE workspaces DROP COLUMN logo_key;
ALTER TABLE organizations DROP COLUMN logo_key;
