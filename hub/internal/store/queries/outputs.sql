-- name: UpsertOutput :one
INSERT INTO outputs (id, workspace_id, task_id, member_id, kind, name, title)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (task_id, name) DO UPDATE SET title = excluded.title, kind = excluded.kind
RETURNING *;

-- name: GetOutput :one
SELECT * FROM outputs WHERE id = $1;

-- name: ListOutputs :many
SELECT * FROM outputs WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: SetOutputStatus :one
UPDATE outputs SET status = $2 WHERE id = $1 RETURNING *;

-- name: CreateOutputVersion :one
INSERT INTO output_versions (id, output_id, number, blob_key, mime, size, note, session_id, model)
VALUES ($1, $2, (SELECT coalesce(max(number), 0) + 1 FROM output_versions v WHERE v.output_id = $2), $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListOutputVersions :many
SELECT * FROM output_versions WHERE output_id = $1 ORDER BY number;

-- name: ListWorkspaceOutputVersions :many
SELECT v.* FROM output_versions v JOIN outputs o ON o.id = v.output_id WHERE o.workspace_id = $1 ORDER BY v.output_id, v.number;

-- name: GetOutputVersion :one
SELECT * FROM output_versions WHERE output_id = $1 AND number = $2;

-- name: MarkOutputSeen :exec
INSERT INTO output_views (user_id, output_id, version) VALUES ($1, $2, $3)
ON CONFLICT (user_id, output_id) DO UPDATE SET version = GREATEST(output_views.version, excluded.version);

-- name: ListOutputViews :many
SELECT v.* FROM output_views v JOIN outputs o ON o.id = v.output_id WHERE o.workspace_id = $1 AND v.user_id = $2;

-- name: GetTaskOutputByName :one
SELECT * FROM outputs WHERE task_id = $1 AND name = $2;

-- name: GetGoalOutputByName :one
SELECT o.* FROM outputs o JOIN tasks t ON t.id = o.task_id WHERE t.goal_id = $1 AND o.name = $2 ORDER BY o.updated_at DESC, o.id LIMIT 1;

-- name: GetLatestOutputVersion :one
SELECT * FROM output_versions WHERE output_id = $1 ORDER BY number DESC LIMIT 1;
