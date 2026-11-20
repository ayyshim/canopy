-- name: CreateOrganization :one
INSERT INTO organizations (id, name, slug, size, about, owner_user_id, default_session_limit, default_weekly_limit)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (slug) DO NOTHING
RETURNING *;

-- name: GetOrganization :one
SELECT * FROM organizations WHERE id = $1;

-- name: UpdateOrganization :one
UPDATE organizations
SET name = coalesce(sqlc.narg(name), name),
    default_session_limit = coalesce(sqlc.narg(default_session_limit), default_session_limit),
    default_weekly_limit = coalesce(sqlc.narg(default_weekly_limit), default_weekly_limit),
    timezone = coalesce(sqlc.narg(timezone), timezone)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: CreateMembership :one
INSERT INTO memberships (id, org_id, user_id, org_role, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetActiveMembership :one
SELECT * FROM memberships WHERE org_id = $1 AND user_id = $2 AND status = 'active';

-- name: ListUserOrganizations :many
SELECT o.id, o.name, o.slug, o.logo_key, m.org_role
FROM memberships m
JOIN organizations o ON o.id = m.org_id
WHERE m.user_id = $1 AND m.status = 'active'
ORDER BY o.name, o.id;

-- name: DeleteOrganization :exec
DELETE FROM organizations WHERE id = $1;

-- name: SetOrganizationLogo :one
UPDATE organizations SET logo_key = sqlc.narg(logo_key) WHERE id = sqlc.arg(id) RETURNING *;
