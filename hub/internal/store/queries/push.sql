-- name: GetVapidKeys :one
SELECT * FROM vapid_keys WHERE id;

-- name: CreateVapidKeys :one
INSERT INTO vapid_keys (public_key, private_key) VALUES ($1, $2)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- A browser endpoint belongs to one person; a new owner takes it over.
-- name: UpsertPushSubscription :one
INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (endpoint) DO UPDATE
SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth, user_agent = EXCLUDED.user_agent
RETURNING *;

-- name: DeletePushSubscription :execrows
DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2;

-- name: DeletePushSubscriptionByEndpoint :exec
DELETE FROM push_subscriptions WHERE endpoint = $1;

-- name: ListUserPushSubscriptions :many
SELECT * FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at;

-- name: TouchPushSubscription :exec
UPDATE push_subscriptions SET last_ok_at = now() WHERE endpoint = $1;

-- name: CreatePushActionToken :exec
INSERT INTO push_action_tokens (token_hash, user_id, signal_id, actions, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- Taking a token deletes it, so it works once. Expired tokens are not returned.
-- name: TakePushActionToken :one
DELETE FROM push_action_tokens WHERE token_hash = $1 AND expires_at > $2
RETURNING *;

-- name: DeleteExpiredPushActionTokens :exec
DELETE FROM push_action_tokens WHERE expires_at <= $1;

-- name: DeleteSignalPushActionTokens :exec
DELETE FROM push_action_tokens WHERE signal_id = $1;

-- name: SetUserSnooze :exec
UPDATE users SET snooze_until = sqlc.narg(snooze_until) WHERE id = sqlc.arg(id);
