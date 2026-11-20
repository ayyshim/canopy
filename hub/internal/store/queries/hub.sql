-- name: GetHubSettings :one
SELECT * FROM hub_settings WHERE id;

-- name: CreateHubSettings :one
-- Inserts the hub's single settings row; no row back means the hub was already set up.
INSERT INTO hub_settings (name, signup_open)
VALUES ($1, $2)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: UpdateHubSettings :one
UPDATE hub_settings
SET name = coalesce(sqlc.narg(name), name),
    signup_open = coalesce(sqlc.narg(signup_open), signup_open)
WHERE id
RETURNING *;

-- name: ListAuthProviders :many
SELECT * FROM auth_providers ORDER BY provider;

-- name: GetAuthProvider :one
SELECT * FROM auth_providers WHERE provider = $1;

-- name: UpsertAuthProvider :one
-- An empty client_secret keeps the stored one.
INSERT INTO auth_providers (provider, enabled, client_id, client_secret)
VALUES (sqlc.arg(provider), sqlc.arg(enabled), sqlc.arg(client_id), sqlc.arg(client_secret))
ON CONFLICT (provider) DO UPDATE
SET enabled = excluded.enabled,
    client_id = excluded.client_id,
    client_secret = CASE WHEN excluded.client_secret = '' THEN auth_providers.client_secret ELSE excluded.client_secret END
RETURNING *;

-- name: SetHubAdmin :exec
UPDATE users SET is_hub_admin = $2 WHERE id = $1;

-- name: CountHubAdmins :one
SELECT count(*) FROM users WHERE is_hub_admin;

-- name: HubTotals :one
SELECT
  (SELECT count(*) FROM organizations)                                          AS organizations,
  (SELECT count(*) FROM users)                                                  AS users,
  (SELECT count(*) FROM users WHERE is_hub_admin)                               AS hub_admins,
  (SELECT count(DISTINCT user_id) FROM auth_sessions WHERE expires_at > now()
     AND last_seen_at > now() - interval '24 hours')                            AS active_users_24h,
  (SELECT count(*) FROM users WHERE created_at > now() - interval '7 days')         AS users_7d,
  (SELECT count(*) FROM organizations WHERE created_at > now() - interval '7 days') AS organizations_7d;

-- name: AdminListOrganizations :many
SELECT o.id, o.name, o.slug, o.size, o.created_at,
       u.id AS owner_id, u.name AS owner_name, u.email AS owner_email,
       (SELECT count(*) FROM memberships m WHERE m.org_id = o.id AND m.status = 'active') AS members
FROM organizations o
JOIN users u ON u.id = o.owner_user_id
WHERE sqlc.arg(q)::text = '' OR o.name ILIKE '%' || sqlc.arg(q)::text || '%' OR o.slug ILIKE '%' || sqlc.arg(q)::text || '%'
ORDER BY o.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: AdminCountOrganizations :one
SELECT count(*) FROM organizations
WHERE sqlc.arg(q)::text = '' OR name ILIKE '%' || sqlc.arg(q)::text || '%' OR slug ILIKE '%' || sqlc.arg(q)::text || '%';

-- name: AdminListOrgMembers :many
SELECT u.id, u.name, u.email, m.org_role, m.status, m.created_at AS joined_at
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.org_id = $1
ORDER BY CASE m.org_role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, u.name;

-- name: AdminListUsers :many
SELECT u.id, u.name, u.email, u.is_hub_admin, u.created_at,
       (SELECT count(*) FROM memberships m WHERE m.user_id = u.id AND m.status = 'active') AS organizations,
       -- epoch means never signed in (sqlc can't type a nullable aggregate here)
       coalesce(s.last_active_at, 'epoch'::timestamptz)::timestamptz AS last_active_at
FROM users u
LEFT JOIN (SELECT user_id, max(last_seen_at)::timestamptz AS last_active_at FROM auth_sessions GROUP BY user_id) s ON s.user_id = u.id
WHERE sqlc.arg(q)::text = '' OR u.name ILIKE '%' || sqlc.arg(q)::text || '%' OR u.email ILIKE '%' || sqlc.arg(q)::text || '%'
ORDER BY u.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: AdminCountUsers :one
SELECT count(*) FROM users
WHERE sqlc.arg(q)::text = '' OR name ILIKE '%' || sqlc.arg(q)::text || '%' OR email ILIKE '%' || sqlc.arg(q)::text || '%';

-- name: AdminListUserOrgs :many
SELECT o.id, o.name, m.org_role
FROM memberships m JOIN organizations o ON o.id = m.org_id
WHERE m.user_id = $1 AND m.status = 'active'
ORDER BY o.name;

-- name: ListHubAuditEvents :many
SELECT * FROM audit_events WHERE org_id IS NULL ORDER BY at DESC LIMIT $1;

-- name: LockHubAdmins :exec
-- Serializes hub admin changes within a transaction so the last admin can't be removed by a race.
SELECT pg_advisory_xact_lock(hashtext('canopy.hub_admins'));

-- name: RecordUpgrade :exec
INSERT INTO hub_upgrades (id, from_version, to_version, from_schema, to_schema, changes, backup_path)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListUpgrades :many
SELECT * FROM hub_upgrades ORDER BY at DESC LIMIT 10;
