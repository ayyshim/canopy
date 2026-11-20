-- name: InsertAuditEvent :exec
INSERT INTO audit_events (id, org_id, workspace_id, actor_user_id, type, summary)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAuditEvents :many
SELECT * FROM audit_events WHERE org_id = $1 ORDER BY at DESC LIMIT $2;

-- name: ListActivity :many
SELECT a.*, u.name AS actor_name, w.name AS workspace_name
FROM audit_events a
LEFT JOIN users u ON u.id = a.actor_user_id
LEFT JOIN workspaces w ON w.id = a.workspace_id
WHERE a.org_id = $1 AND a.at < $2
  AND (sqlc.arg(prefix)::text = '' OR a.type LIKE sqlc.arg(prefix)::text || '%' OR a.type = ANY(sqlc.arg(types)::text[]))
  AND (sqlc.narg(actor)::uuid IS NULL OR a.actor_user_id = sqlc.narg(actor)::uuid)
ORDER BY a.at DESC, a.id DESC
LIMIT $3;

-- name: PruneAudit :exec
DELETE FROM audit_events WHERE at < now() - interval '400 days';

-- name: PruneLogs :exec
DELETE FROM log_lines WHERE at < now() - interval '90 days';
