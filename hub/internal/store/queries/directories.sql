-- name: CreateDirectory :one
INSERT INTO directories (id, workspace_id, path, name, kind, owner_user_id, machine_id, repo_remote, default_branch)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetDirectory :one
SELECT * FROM directories WHERE id = $1;

-- name: ListDirectories :many
SELECT * FROM directories WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: DeleteDirectory :exec
DELETE FROM directories WHERE id = $1;

-- name: UpdateDirectory :one
UPDATE directories
SET name = coalesce(sqlc.narg(name), name), kind = coalesce(sqlc.narg(kind), kind),
    repo_remote = coalesce(sqlc.narg(repo_remote), repo_remote),
    default_branch = coalesce(sqlc.narg(default_branch), default_branch),
    setup_command = coalesce(sqlc.narg(setup_command), setup_command),
    copy_files = coalesce(sqlc.narg(copy_files), copy_files)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: BindUnboundDirectories :many
-- Directories added by a person before their machine connected get bound to it.
UPDATE directories d SET machine_id = $2
FROM workspaces w
WHERE d.workspace_id = w.id AND d.owner_user_id = $1 AND d.machine_id IS NULL AND w.org_id = $3
RETURNING d.*;

-- name: ListDirectoryAccess :many
SELECT da.* FROM directory_access da JOIN directories d ON d.id = da.directory_id
WHERE d.workspace_id = $1;

-- name: UpsertDirectoryAccess :one
INSERT INTO directory_access (id, directory_id, user_id, mode, granted_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (directory_id, user_id) DO UPDATE SET mode = excluded.mode, granted_by = excluded.granted_by
RETURNING *;

-- name: DeleteDirectoryAccess :exec
DELETE FROM directory_access WHERE directory_id = $1 AND user_id = $2;

-- name: UpsertClone :one
INSERT INTO directory_clones (id, directory_id, runner_id, user_id, path, current_branch, dirty)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (directory_id, runner_id) DO UPDATE
SET path = excluded.path, user_id = excluded.user_id, current_branch = excluded.current_branch, dirty = excluded.dirty
RETURNING *;

-- name: ListWorkspaceClones :many
SELECT c.* FROM directory_clones c JOIN directories d ON d.id = c.directory_id
WHERE d.workspace_id = $1 ORDER BY c.created_at;

-- name: DeleteClone :exec
DELETE FROM directory_clones WHERE directory_id = $1 AND runner_id = $2;
