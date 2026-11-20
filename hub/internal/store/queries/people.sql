-- name: ListOrgPeople :many
SELECT u.id, u.name, u.email, u.avatar_color, m.org_role, m.status, m.created_at AS joined_at,
       m.personal_session_limit, m.personal_weekly_limit
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.org_id = $1
ORDER BY CASE m.org_role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, u.name;

-- name: GetMembership :one
SELECT * FROM memberships WHERE org_id = $1 AND user_id = $2;

-- name: UpdateMembership :one
UPDATE memberships
SET org_role = coalesce(sqlc.narg(org_role), org_role),
    personal_session_limit = CASE WHEN sqlc.arg(set_session_limit)::boolean THEN sqlc.narg(personal_session_limit) ELSE personal_session_limit END,
    personal_weekly_limit = CASE WHEN sqlc.arg(set_weekly_limit)::boolean THEN sqlc.narg(personal_weekly_limit) ELSE personal_weekly_limit END
WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteMembership :exec
DELETE FROM memberships WHERE org_id = $1 AND user_id = $2;

-- name: DeleteUserWorkspaceAccessInOrg :exec
DELETE FROM workspace_access a USING workspaces w
WHERE a.workspace_id = w.id AND w.org_id = $1 AND a.user_id = $2;
