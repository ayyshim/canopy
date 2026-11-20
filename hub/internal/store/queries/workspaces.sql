-- name: CreateWorkspace :one
INSERT INTO workspaces (id, org_id, name, about, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM workspaces WHERE id = $1;

-- name: ListOrgWorkspaces :many
SELECT * FROM workspaces WHERE org_id = $1 ORDER BY name, id;

-- name: ListUserWorkspaces :many
SELECT w.* FROM workspaces w
JOIN workspace_access a ON a.workspace_id = w.id AND a.user_id = $2
WHERE w.org_id = $1
ORDER BY w.name, w.id;

-- name: CountOrgWorkspaces :one
SELECT count(*) FROM workspaces WHERE org_id = $1;

-- name: UpdateWorkspace :one
UPDATE workspaces
SET name = coalesce(sqlc.narg(name), name),
    about = coalesce(sqlc.narg(about), about),
    goal_completion = coalesce(sqlc.narg(goal_completion), goal_completion)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteWorkspace :exec
DELETE FROM workspaces WHERE id = $1;

-- name: WorkspaceCounts :one
SELECT
  (SELECT count(*) FROM members m WHERE m.workspace_id = $1)        AS members,
  (SELECT count(*) FROM directories d WHERE d.workspace_id = $1)    AS directories,
  (SELECT count(*) FROM workspace_access a WHERE a.workspace_id = $1) AS people;

-- name: GetWorkspaceAccess :one
SELECT * FROM workspace_access WHERE workspace_id = $1 AND user_id = $2;

-- name: ListWorkspaceAccess :many
SELECT a.*, u.name AS user_name, u.email AS user_email, u.avatar_color AS user_avatar_color
FROM workspace_access a JOIN users u ON u.id = a.user_id
WHERE a.workspace_id = $1
ORDER BY u.name;

-- name: UpsertWorkspaceAccess :one
INSERT INTO workspace_access (id, workspace_id, user_id, role, manager_ids, independent_staff)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, user_id) DO UPDATE
SET role = excluded.role, manager_ids = excluded.manager_ids, independent_staff = excluded.independent_staff
RETURNING *;

-- name: DeleteWorkspaceAccess :exec
DELETE FROM workspace_access WHERE workspace_id = $1 AND user_id = $2;

-- name: ListUserWorkspaceAccessInOrg :many
SELECT a.*, w.name AS workspace_name
FROM workspace_access a JOIN workspaces w ON w.id = a.workspace_id
WHERE w.org_id = $1 AND a.user_id = $2
ORDER BY w.name;
