-- name: CreateAgentSession :one
INSERT INTO sessions (id, workspace_id, member_id, number, status, runner_id, cli_session_id, task_id, recovered_from, restarts, goal_id, person_id)
VALUES ($1, $2, $3, (SELECT coalesce(max(number), 0) + 1 FROM sessions s WHERE s.member_id = $3), $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetAgentSession :one
SELECT * FROM sessions WHERE id = $1;

-- name: LatestMemberSession :one
SELECT * FROM sessions WHERE member_id = $1 ORDER BY number DESC LIMIT 1;

-- name: ListLatestSessions :many
-- The newest session for each member and goal (or member and person).
SELECT DISTINCT ON (member_id, goal_id, person_id) * FROM sessions
WHERE workspace_id = $1 ORDER BY member_id, goal_id, person_id, number DESC;

-- name: ListMemberLiveSessions :many
SELECT * FROM sessions WHERE member_id = $1 AND status IN ('starting', 'running', 'waiting', 'gate', 'review');

-- name: ListGoalLiveSessions :many
SELECT * FROM sessions WHERE goal_id = $1 AND status IN ('starting', 'running', 'waiting', 'gate', 'review', 'asleep');

-- name: ListMemberSessions :many
SELECT * FROM sessions WHERE member_id = $1 ORDER BY number DESC LIMIT $2;

-- name: ListLiveSessionsOnRunner :many
SELECT * FROM sessions WHERE runner_id = $1 AND status NOT IN ('ended', 'crashed', 'error', 'asleep');

-- name: UpdateAgentSession :one
UPDATE sessions
SET status = coalesce(sqlc.narg(status), status),
    cli_session_id = coalesce(sqlc.narg(cli_session_id), cli_session_id),
    task_id = CASE WHEN sqlc.arg(set_task)::boolean THEN sqlc.narg(task_id) ELSE task_id END,
    tokens_in = tokens_in + sqlc.arg(add_in)::bigint,
    tokens_out = tokens_out + sqlc.arg(add_out)::bigint,
    last_active_at = now(),
    ended_at = CASE WHEN sqlc.arg(set_ended)::boolean THEN now() ELSE ended_at END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: IdleSessions :many
SELECT * FROM sessions WHERE status = 'waiting' AND last_active_at < $1;

-- name: InsertLogLine :one
INSERT INTO log_lines (workspace_id, session_id, member_id, task_id, kind, text)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListMemberLog :many
SELECT * FROM log_lines WHERE member_id = $1 ORDER BY id DESC LIMIT $2;

-- name: ListTaskLog :many
SELECT * FROM log_lines WHERE task_id = $1 ORDER BY id DESC LIMIT $2;

-- name: SetSessionMounts :one
UPDATE sessions SET mounts = $2, cwd = $3, runner_id = $4 WHERE id = $1 RETURNING *;

-- name: SetSessionStatus :one
UPDATE sessions SET status = $2, last_active_at = now(),
  ended_at = CASE WHEN $2 IN ('ended', 'crashed', 'error') THEN now() ELSE ended_at END
WHERE id = $1 RETURNING *;
