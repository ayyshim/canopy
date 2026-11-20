-- name: CreateAttachment :one
INSERT INTO attachments (id, workspace_id, goal_id, task_id, name, mime, size, sha256, user_id, member_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = $1;

-- name: ListWorkspaceAttachments :many
SELECT * FROM attachments WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: ListAttachmentsFor :many
SELECT * FROM attachments
WHERE (goal_id IS NOT NULL AND goal_id = sqlc.narg(goal_id)::uuid) OR task_id = ANY(sqlc.arg(task_ids)::uuid[])
ORDER BY created_at, id;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = $1;
