-- name: CreateJob :one
INSERT INTO jobs (id, workspace_id, title, kind, brief, owner_user_id, manager_id, assignee_member_id, runner_id,
  cron, timezone, next_run_at, missed_policy, catchup_window_secs, run_timeout_secs, token_cap_per_run,
  token_cap_per_month, notify, ends_at, max_runs, required_dirs, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = $1;

-- name: ListJobs :many
SELECT * FROM jobs WHERE workspace_id = $1 ORDER BY created_at DESC, id;

-- name: UpdateJob :one
UPDATE jobs SET title = coalesce(sqlc.narg(title), title), brief = coalesce(sqlc.narg(brief), brief),
  status = coalesce(sqlc.narg(status), status), pause_reason = coalesce(sqlc.narg(pause_reason), pause_reason),
  owner_user_id = coalesce(sqlc.narg(owner_user_id), owner_user_id),
  assignee_member_id = CASE WHEN sqlc.arg(set_assignee)::boolean THEN sqlc.narg(assignee_member_id) ELSE assignee_member_id END,
  runner_id = CASE WHEN sqlc.arg(set_runner)::boolean THEN sqlc.narg(runner_id) ELSE runner_id END,
  cron = coalesce(sqlc.narg(cron), cron), timezone = coalesce(sqlc.narg(timezone), timezone),
  next_run_at = CASE WHEN sqlc.arg(set_next)::boolean THEN sqlc.narg(next_run_at) ELSE next_run_at END,
  last_run_at = coalesce(sqlc.narg(last_run_at), last_run_at),
  missed_policy = coalesce(sqlc.narg(missed_policy), missed_policy),
  catchup_window_secs = coalesce(sqlc.narg(catchup_window_secs), catchup_window_secs),
  run_timeout_secs = coalesce(sqlc.narg(run_timeout_secs), run_timeout_secs),
  token_cap_per_run = CASE WHEN sqlc.arg(set_caps)::boolean THEN sqlc.narg(token_cap_per_run) ELSE token_cap_per_run END,
  token_cap_per_month = CASE WHEN sqlc.arg(set_caps)::boolean THEN sqlc.narg(token_cap_per_month) ELSE token_cap_per_month END,
  notify = coalesce(sqlc.narg(notify), notify),
  ends_at = CASE WHEN sqlc.arg(set_ends)::boolean THEN sqlc.narg(ends_at) ELSE ends_at END,
  max_runs = CASE WHEN sqlc.arg(set_ends)::boolean THEN sqlc.narg(max_runs) ELSE max_runs END,
  required_dirs = coalesce(sqlc.narg(required_dirs), required_dirs),
  manager_id = coalesce(sqlc.narg(manager_id), manager_id),
  memory_note_id = coalesce(sqlc.narg(memory_note_id), memory_note_id),
  consecutive_failures = coalesce(sqlc.narg(consecutive_failures), consecutive_failures)
WHERE id = sqlc.arg(id) RETURNING *;

-- name: DeleteJob :exec
DELETE FROM jobs WHERE id = $1;

-- name: DueJobs :many
-- Locks the rows so two hubs never start the same run.
SELECT * FROM jobs
WHERE status = 'active' AND next_run_at IS NOT NULL AND next_run_at <= sqlc.arg(now)
ORDER BY next_run_at, id
LIMIT sqlc.arg(max_rows)
FOR UPDATE SKIP LOCKED;

-- name: NextJobRunNumber :one
SELECT coalesce(max(number), 0)::int + 1 FROM job_runs WHERE job_id = $1;

-- name: CreateJobRun :one
INSERT INTO job_runs (id, job_id, number, scheduled_for, trigger, status, skip_reason, attempt, goal_id, started_at, finished_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, $9, $10)
RETURNING *;

-- name: GetJobRun :one
SELECT * FROM job_runs WHERE id = $1;

-- name: UpdateJobRun :one
UPDATE job_runs SET status = coalesce(sqlc.narg(status), status), skip_reason = coalesce(sqlc.narg(skip_reason), skip_reason),
  attempt = coalesce(sqlc.narg(attempt), attempt), goal_id = coalesce(sqlc.narg(goal_id), goal_id),
  started_at = coalesce(sqlc.narg(started_at), started_at), finished_at = coalesce(sqlc.narg(finished_at), finished_at),
  tokens = coalesce(sqlc.narg(tokens), tokens), result = coalesce(sqlc.narg(result), result),
  title = coalesce(sqlc.narg(title), title), summary = coalesce(sqlc.narg(summary), summary),
  error = coalesce(sqlc.narg(error), error),
  pending_memory = CASE WHEN sqlc.arg(set_pending)::boolean THEN sqlc.narg(pending_memory) ELSE pending_memory END,
  memory_after = CASE WHEN sqlc.arg(set_memory_after)::boolean THEN sqlc.narg(memory_after) ELSE memory_after END
WHERE id = sqlc.arg(id) RETURNING *;

-- name: RunningJobRun :one
SELECT * FROM job_runs WHERE job_id = $1 AND status = 'running';

-- name: LastJobRuns :many
SELECT * FROM job_runs WHERE job_id = $1 ORDER BY number DESC LIMIT $2;

-- name: JobRunByGoal :one
SELECT r.* FROM job_runs r JOIN goals g ON g.job_run_id = r.id WHERE g.id = $1;

-- name: DueJobIDs :many
SELECT id FROM jobs
WHERE status = 'active' AND next_run_at IS NOT NULL AND next_run_at <= sqlc.arg(now)
ORDER BY next_run_at, id
LIMIT sqlc.arg(max_rows);

-- name: LockDueJob :one
-- One due job, locked; no row when another hub holds it or it is no longer due.
SELECT * FROM jobs
WHERE id = sqlc.arg(id) AND status = 'active' AND next_run_at IS NOT NULL AND next_run_at <= sqlc.arg(now)
FOR UPDATE SKIP LOCKED;

-- name: LockJob :one
SELECT * FROM jobs WHERE id = $1 FOR UPDATE;

-- name: CountJobRuns :one
-- Runs that count toward max_runs: not skipped, not a test.
SELECT count(*)::int FROM job_runs WHERE job_id = $1 AND status <> 'skipped' AND trigger <> 'test';

-- name: RunningJobRuns :many
SELECT * FROM job_runs WHERE status = 'running' ORDER BY started_at;

-- name: GoalTokens :one
SELECT coalesce(sum(tokens_in + tokens_out), 0)::bigint FROM usage_work WHERE goal_id = $1;

-- name: GoalStartedTasks :one
SELECT count(*)::int FROM tasks WHERE goal_id = $1 AND status IN ('doing', 'review', 'done');
