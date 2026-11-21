-- +goose Up
-- Isolated staff made before creators were recorded go to the workspace creator, so someone can still chat.
UPDATE members m SET created_by = w.created_by
FROM workspaces w
WHERE m.workspace_id = w.id AND m.isolated AND m.created_by IS NULL;

-- +goose Down
SELECT 1;
