-- name: CreateUser :one
INSERT INTO users (id, email, name, password_hash, avatar_color, last_org_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserProfile :one
UPDATE users
SET name = coalesce(sqlc.narg(name), name),
    avatar_color = coalesce(sqlc.narg(avatar_color), avatar_color)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetUserLastOrg :exec
UPDATE users SET last_org_id = $2 WHERE id = $1;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: SetUserNotifications :exec
UPDATE users SET notifications = $2, updated_at = now() WHERE id = $1;

-- name: SetUserDailySent :exec
UPDATE users SET daily_sent_on = $2 WHERE id = $1;

-- name: ListDailySummaryUsers :many
-- People who asked for the daily summary email and haven't had today's.
SELECT u.*, coalesce(o.timezone, 'UTC')::text AS timezone FROM users u
LEFT JOIN organizations o ON o.id = u.last_org_id
WHERE (u.notifications -> 'daily' ->> 'email')::boolean IS TRUE;
