-- name: InsertNotification :one
INSERT INTO notification_outbox (id, org_id, workspace_id, member_id, kind, title, body, link, signal_id, task_id, goal_id, for_user_id, recipients, media)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notification_outbox WHERE workspace_id = $1 ORDER BY created_at, id;

-- Rows whose Discord send is due. The lock keeps two hubs from sending the same row.
-- name: ClaimDiscordNotifications :many
SELECT * FROM notification_outbox
WHERE discord_sent_at IS NULL AND discord_attempts < $1 AND discord_next_at <= $2
ORDER BY created_at, id
LIMIT $3
FOR UPDATE SKIP LOCKED;

-- name: ClaimPushNotifications :many
SELECT * FROM notification_outbox
WHERE push_sent_at IS NULL AND push_attempts < $1 AND push_next_at <= $2
ORDER BY created_at, id
LIMIT $3
FOR UPDATE SKIP LOCKED;

-- name: MarkDiscordSent :exec
UPDATE notification_outbox SET discord_sent_at = now(), discord_last_error = '' WHERE id = $1;

-- name: MarkDiscordFailed :exec
UPDATE notification_outbox
SET discord_attempts = discord_attempts + 1, discord_last_error = sqlc.arg(last_error), discord_next_at = sqlc.arg(next_at)
WHERE id = sqlc.arg(id);

-- name: MarkPushSent :exec
UPDATE notification_outbox SET push_sent_at = now(), push_last_error = '' WHERE id = $1;

-- name: MarkPushFailed :exec
UPDATE notification_outbox
SET push_attempts = push_attempts + 1, push_last_error = sqlc.arg(last_error), push_next_at = sqlc.arg(next_at)
WHERE id = sqlc.arg(id);

-- name: DeleteOldNotifications :execrows
DELETE FROM notification_outbox WHERE created_at < $1;

-- name: SetWorkspaceDiscord :one
UPDATE workspaces
SET discord_webhook = sqlc.arg(webhook), discord_kinds = sqlc.arg(kinds), discord_role_id = sqlc.arg(role_id),
    discord_forum = sqlc.arg(forum), discord_last_error = CASE WHEN sqlc.arg(webhook)::text = '' THEN '' ELSE discord_last_error END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: SetWorkspaceDiscordKeepWebhook :one
UPDATE workspaces
SET discord_kinds = sqlc.arg(kinds), discord_role_id = sqlc.arg(role_id), discord_forum = sqlc.arg(forum)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ClearWorkspaceDiscord :one
UPDATE workspaces
SET discord_webhook = '', discord_role_id = '', discord_forum = false, discord_last_ok_at = NULL, discord_last_error = ''
WHERE id = $1
RETURNING *;

-- name: SetWorkspaceDiscordStatus :exec
UPDATE workspaces
SET discord_last_ok_at = CASE WHEN sqlc.arg(error)::text = '' THEN now() ELSE discord_last_ok_at END,
    discord_last_error = sqlc.arg(error)
WHERE id = sqlc.arg(id);

-- name: CountAgentNotificationsForTask :one
SELECT count(*) FROM notification_outbox WHERE task_id = $1 AND media @> '{"agent": true}' AND created_at > $2;

-- name: CountAgentNotificationsForMember :one
SELECT count(*) FROM notification_outbox WHERE member_id = $1 AND media @> '{"agent": true}' AND created_at > $2;

-- name: SetGoalDiscordThread :exec
UPDATE goals SET discord_thread_id = $2 WHERE id = $1;

-- Turns Discord off after the webhook was deleted; the settings stay.
-- name: DisableWorkspaceDiscord :exec
UPDATE workspaces SET discord_webhook = '', discord_last_error = sqlc.arg(error) WHERE id = sqlc.arg(id);

-- name: OpenWorkspaceDiscordSignal :one
SELECT * FROM signals WHERE workspace_id = $1 AND type = 'error' AND status = 'open' AND payload->>'source' = 'discord' LIMIT 1;
