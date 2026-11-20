-- name: CreateSignal :one
INSERT INTO signals (id, workspace_id, type, member_id, task_id, payload, request_id, expires_at, session_id, for_user_id, goal_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetSignal :one
SELECT * FROM signals WHERE id = $1;

-- name: GetSignalForUpdate :one
SELECT * FROM signals WHERE id = $1 FOR UPDATE;

-- name: ListOpenSignals :many
SELECT * FROM signals WHERE workspace_id = $1 AND status = 'open' ORDER BY created_at;

-- name: ListTaskOpenSignals :many
SELECT * FROM signals WHERE task_id = $1 AND status = 'open' ORDER BY created_at;

-- name: ResolveSignal :one
UPDATE signals SET status = sqlc.arg(status), resolved_by = sqlc.narg(resolved_by), resolution = sqlc.arg(resolution), resolved_at = now()
WHERE id = sqlc.arg(id) AND status = 'open'
RETURNING *;

-- name: ExpiredSignals :many
SELECT * FROM signals WHERE status = 'open' AND expires_at IS NOT NULL AND expires_at <= $1;

-- name: OpenSignalsForMember :many
SELECT * FROM signals WHERE member_id = $1 AND status = 'open';

-- name: OpenAccountSignal :one
SELECT * FROM signals WHERE workspace_id = $1 AND type = $2 AND status = 'open'
  AND payload->>'cli' = sqlc.arg(cli)::text AND payload->>'runner_id' = sqlc.arg(runner_id)::text LIMIT 1;

-- name: OpenGoalSignal :one
SELECT * FROM signals WHERE goal_id = $1 AND type = $2 AND status = 'open' LIMIT 1;

-- name: GetGateRule :one
SELECT * FROM gate_rules WHERE member_id = $1 AND action_kind = $2;

-- name: CreateGateRule :exec
INSERT INTO gate_rules (id, member_id, action_kind, created_by) VALUES ($1, $2, $3, $4)
ON CONFLICT (member_id, action_kind) DO NOTHING;

-- name: UpdateSignalPayload :one
UPDATE signals SET payload = $2 WHERE id = $1 RETURNING *;

-- name: OpenSignalsForSession :many
SELECT * FROM signals WHERE session_id = $1 AND status = 'open';

-- name: ListUnmailedSignals :many
-- Open signals older than the cutoff that haven't been emailed yet.
SELECT * FROM signals WHERE status = 'open' AND emailed_at IS NULL AND created_at < $1 ORDER BY created_at LIMIT 200;

-- name: MarkSignalEmailed :exec
UPDATE signals SET emailed_at = now() WHERE id = $1;
