-- name: InsertUsageEvent :one
INSERT INTO usage_events (org_id, workspace_id, member_id, attributed_user_id, session_id, cli, model, tokens_in, tokens_out)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: AddUsageHourly :exec
INSERT INTO usage_hourly (org_id, hour, user_id, member_id, workspace_id, model, tokens_in, tokens_out, calls)
VALUES ($1, date_trunc('hour', sqlc.arg(at)::timestamptz), $2, $3, $4, $5, $6, $7, 1)
ON CONFLICT (org_id, hour, user_id, member_id, workspace_id, model)
DO UPDATE SET tokens_in = usage_hourly.tokens_in + excluded.tokens_in,
              tokens_out = usage_hourly.tokens_out + excluded.tokens_out,
              calls = usage_hourly.calls + 1;

-- name: UserTokensSince :one
SELECT coalesce(sum(tokens_in + tokens_out), 0)::bigint FROM usage_events
WHERE org_id = $1 AND attributed_user_id = $2 AND at >= $3;

-- name: AddAccountTokens :one
UPDATE accounts
SET used_tokens = CASE WHEN used_day = current_date THEN used_tokens + sqlc.arg(tokens)::bigint ELSE sqlc.arg(tokens)::bigint END,
    used_day = current_date
WHERE runner_id = sqlc.arg(runner_id) AND cli = sqlc.arg(cli)
RETURNING *;

-- name: UpdateAccountState :one
UPDATE accounts
SET limited_until = CASE WHEN sqlc.arg(set_limited)::boolean THEN sqlc.narg(limited_until) ELSE limited_until END,
    paused = coalesce(sqlc.narg(paused), paused),
    utilization = coalesce(sqlc.narg(utilization), utilization)
WHERE runner_id = sqlc.arg(runner_id) AND cli = sqlc.arg(cli)
RETURNING *;

-- name: CreateQueuedWork :one
INSERT INTO queued_work (id, org_id, workspace_id, user_id, kind, payload, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListQueuedWork :many
SELECT * FROM queued_work ORDER BY created_at;

-- name: ListUserQueuedWork :many
SELECT * FROM queued_work WHERE user_id = $1 ORDER BY created_at;

-- name: DeleteQueuedWork :exec
DELETE FROM queued_work WHERE id = $1;

-- name: SetAccountUseKey :one
UPDATE accounts SET use_key_until = $3 WHERE runner_id = $1 AND cli = $2 RETURNING *;

-- name: SetAccountCapWarned :exec
UPDATE accounts SET cap_warned_day = current_date WHERE runner_id = $1 AND cli = $2;

-- name: SetAccountPaused :one
UPDATE accounts SET paused = $3 WHERE runner_id = $1 AND cli = $2 RETURNING *;

-- name: AccountsToRefresh :many
-- Limits whose reset time passed, and caps paused on a previous day.
SELECT * FROM accounts
WHERE (limited_until IS NOT NULL AND limited_until <= now())
   OR (paused AND used_day < current_date)
   OR (use_key_until IS NOT NULL AND use_key_until <= now());

-- name: ClearAccountLimit :one
UPDATE accounts SET limited_until = NULL,
  paused = CASE WHEN used_day < current_date THEN false ELSE paused END,
  used_tokens = CASE WHEN used_day < current_date THEN 0 ELSE used_tokens END,
  used_day = current_date,
  use_key_until = CASE WHEN use_key_until <= now() THEN NULL ELSE use_key_until END
WHERE id = $1 RETURNING *;

-- name: ListOrgWorkspacesUsingCLI :many
SELECT DISTINCT w.* FROM workspaces w JOIN members m ON m.workspace_id = w.id
WHERE w.org_id = $1 AND m.cli = $2;

-- name: ListRunnerLiveSessions :many
SELECT * FROM sessions WHERE runner_id = $1 AND status IN ('starting', 'running', 'waiting', 'gate', 'review');

-- name: OldestUserUsageSince :one
SELECT coalesce(min(at), now())::timestamptz FROM usage_events WHERE org_id = $1 AND attributed_user_id = $2 AND at >= $3;

-- name: UsageRows :many
SELECT * FROM usage_hourly WHERE org_id = $1 AND hour >= $2 AND hour < $3
  AND (sqlc.narg(workspace_id)::uuid IS NULL OR workspace_id = sqlc.narg(workspace_id)::uuid);

-- name: HubUsageRows :many
SELECT org_id, hour, sum(tokens_in + tokens_out)::bigint AS tokens FROM usage_hourly WHERE hour >= $1 GROUP BY org_id, hour;

-- name: LiveSessionCount :one
SELECT count(*) FROM sessions s JOIN workspaces w ON w.id = s.workspace_id
WHERE w.org_id = $1 AND s.status IN ('starting', 'running', 'waiting', 'gate', 'review')
  AND (sqlc.narg(workspace_id)::uuid IS NULL OR s.workspace_id = sqlc.narg(workspace_id)::uuid);

-- name: HubLiveSessionCount :one
SELECT count(*) FROM sessions WHERE status IN ('starting', 'running', 'waiting', 'gate', 'review');

-- name: GoalsDoneBetween :one
SELECT count(*) FROM goals g JOIN workspaces w ON w.id = g.workspace_id
WHERE w.org_id = $1 AND g.finished_at >= $2 AND g.finished_at < $3
  AND (sqlc.narg(workspace_id)::uuid IS NULL OR g.workspace_id = sqlc.narg(workspace_id)::uuid);

-- name: SignalStats :one
SELECT
  count(*) FILTER (WHERE s.status = 'open')::bigint AS open,
  count(*) FILTER (WHERE s.resolved_at >= $2 AND s.resolved_at < $3 AND s.resolved_by IS NOT NULL)::bigint AS decisions,
  coalesce(avg(extract(epoch FROM s.resolved_at - s.created_at)) FILTER (WHERE s.resolved_at >= $2 AND s.resolved_at < $3 AND s.resolved_by IS NOT NULL), 0)::float8 AS avg_answer_seconds
FROM signals s JOIN workspaces w ON w.id = s.workspace_id
WHERE w.org_id = $1 AND (sqlc.narg(workspace_id)::uuid IS NULL OR s.workspace_id = sqlc.narg(workspace_id)::uuid);

-- name: OutputsMadeBetween :one
SELECT count(*) FROM output_versions v JOIN outputs o ON o.id = v.output_id JOIN workspaces w ON w.id = o.workspace_id
WHERE w.org_id = $1 AND v.created_at >= $2 AND v.created_at < $3
  AND (sqlc.narg(workspace_id)::uuid IS NULL OR o.workspace_id = sqlc.narg(workspace_id)::uuid);

-- name: PersonCounts :many
-- Per person in a period: goals given, decisions, outputs reviewed, messages to agents, answer time.
SELECT u.id AS user_id,
  (SELECT count(*) FROM goals g JOIN workspaces w ON w.id = g.workspace_id WHERE w.org_id = $1 AND g.created_by = u.id AND g.created_at >= $2 AND g.created_at < $3)::bigint AS goals,
  (SELECT count(*) FROM signals s JOIN workspaces w ON w.id = s.workspace_id WHERE w.org_id = $1 AND s.resolved_by = u.id AND s.resolved_at >= $2 AND s.resolved_at < $3)::bigint AS decisions,
  (SELECT count(*) FROM signals s JOIN workspaces w ON w.id = s.workspace_id WHERE w.org_id = $1 AND s.type = 'review' AND s.resolved_by = u.id AND s.resolved_at >= $2 AND s.resolved_at < $3)::bigint AS reviews,
  (SELECT count(*) FROM chat_messages c JOIN workspaces w ON w.id = c.workspace_id WHERE w.org_id = $1 AND c.author_user_id = u.id AND c.created_at >= $2 AND c.created_at < $3)::bigint AS messages,
  (SELECT coalesce(avg(extract(epoch FROM s.resolved_at - s.created_at)), 0) FROM signals s JOIN workspaces w ON w.id = s.workspace_id WHERE w.org_id = $1 AND s.resolved_by = u.id AND s.resolved_at >= $2 AND s.resolved_at < $3)::float8 AS avg_answer_seconds,
  (SELECT max(last_seen_at) FROM auth_sessions a WHERE a.user_id = u.id)::timestamptz AS last_active
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.org_id = $1 AND m.status = 'active';

-- name: MemberStats :many
SELECT m.id AS member_id,
  (SELECT count(*) FROM sessions ss WHERE ss.member_id = m.id AND ss.started_at >= $2 AND ss.started_at < $3)::bigint AS session_count,
  (SELECT count(*) FROM tasks t WHERE t.member_id = m.id AND t.status = 'done' AND t.finished_at >= $2 AND t.finished_at < $3)::bigint AS tasks_done,
  (SELECT coalesce(avg(extract(epoch FROM t.finished_at - t.started_at)), 0) FROM tasks t WHERE t.member_id = m.id AND t.status = 'done' AND t.finished_at >= $2 AND t.finished_at < $3)::float8 AS avg_task_seconds,
  (SELECT count(*) FROM signals s WHERE s.member_id = m.id AND s.type = 'error' AND s.created_at >= $2 AND s.created_at < $3)::bigint AS errors
FROM members m JOIN workspaces w ON w.id = m.workspace_id
WHERE w.org_id = $1;

-- name: PersonSessions :many
SELECT u.session_id, min(u.at)::timestamptz AS started, max(u.at)::timestamptz AS ended, sum(u.tokens_in + u.tokens_out)::bigint AS tokens,
  u.workspace_id, u.member_id
FROM usage_events u
WHERE u.org_id = $1 AND u.attributed_user_id = $2 AND u.at >= $3
GROUP BY u.session_id, u.workspace_id, u.member_id
ORDER BY started DESC LIMIT 50;

-- name: OrgMembersAll :many
SELECT m.*, w.name AS workspace_name FROM members m JOIN workspaces w ON w.id = m.workspace_id WHERE w.org_id = $1;

-- name: ListRunnerSessionsForResume :many
-- The newest session per member and goal (or person) on a computer, live or asleep.
SELECT DISTINCT ON (member_id, goal_id, person_id) * FROM sessions
WHERE runner_id = $1 AND status IN ('starting', 'running', 'waiting', 'gate', 'review', 'asleep')
ORDER BY member_id, goal_id, person_id, number DESC;
