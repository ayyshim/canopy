-- name: InsertEvent :one
INSERT INTO events (org_id, workspace_id, type, member_id, admin_only, data)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING seq, at;

-- name: MaxEventSeq :one
SELECT coalesce(max(seq), 0)::bigint FROM events;

-- name: MaxWorkspaceSeq :one
SELECT coalesce(max(seq), 0)::bigint FROM events WHERE workspace_id = $1 OR (org_id = $2 AND workspace_id IS NULL);

-- name: EventsSince :many
SELECT * FROM events WHERE seq > $1 AND (workspace_id = $2 OR (org_id = $3 AND workspace_id IS NULL)) ORDER BY seq LIMIT 5000;
