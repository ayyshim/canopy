-- name: CreateSession :one
INSERT INTO auth_sessions (id, user_id, token_hash, expires_at, user_agent, ip)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetLiveSession :one
SELECT * FROM auth_sessions WHERE token_hash = $1 AND expires_at > now();

-- name: ExtendSession :exec
UPDATE auth_sessions SET expires_at = $2, last_seen_at = now() WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM auth_sessions WHERE id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM auth_sessions WHERE user_id = $1;

-- name: DeleteOtherUserSessions :exec
DELETE FROM auth_sessions WHERE user_id = $1 AND id <> $2;
