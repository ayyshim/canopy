-- name: CreateGoal :one
INSERT INTO goals (id, workspace_id, manager_id, title, done_when, color, created_by, runner_id, base, branch, snapshot)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetGoal :one
SELECT * FROM goals WHERE id = $1;

-- name: ListGoals :many
SELECT * FROM goals WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: CountGoals :one
SELECT count(*) FROM goals WHERE workspace_id = $1;

-- name: UpdateGoal :one
UPDATE goals
SET status = coalesce(sqlc.narg(status), status),
    summary = coalesce(sqlc.narg(summary), summary),
    manager_id = CASE WHEN sqlc.arg(set_manager)::boolean THEN sqlc.narg(manager_id) ELSE manager_id END,
    planned_at = CASE WHEN sqlc.arg(set_planned)::boolean THEN now() ELSE planned_at END,
    finished_at = CASE WHEN sqlc.arg(set_finished)::boolean THEN now() ELSE finished_at END,
    runner_id = CASE WHEN sqlc.arg(set_runner)::boolean THEN sqlc.narg(runner_id) ELSE runner_id END,
    shipped = coalesce(sqlc.narg(shipped), shipped),
    show_tasks = coalesce(sqlc.narg(show_tasks), show_tasks),
    done_by = CASE WHEN sqlc.arg(set_done_by)::boolean THEN sqlc.narg(done_by) ELSE done_by END,
    ready_at = CASE WHEN sqlc.arg(set_ready)::boolean THEN now() ELSE ready_at END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: InsertGoalEvent :one
INSERT INTO goal_events (id, goal_id, type, summary, refs)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListGoalEvents :many
SELECT * FROM goal_events WHERE goal_id = $1 ORDER BY at, id;

-- name: ListWorkspaceGoalEvents :many
SELECT e.* FROM goal_events e JOIN goals g ON g.id = e.goal_id WHERE g.workspace_id = $1 ORDER BY e.at, e.id;

-- name: UpsertGoalRepo :one
INSERT INTO goal_repos (goal_id, directory_id, base, base_sha)
VALUES ($1, $2, $3, $4)
ON CONFLICT (goal_id, directory_id) DO UPDATE SET base = excluded.base
RETURNING *;

-- name: ListGoalRepos :many
SELECT * FROM goal_repos WHERE goal_id = $1 ORDER BY created_at;

-- name: ListWorkspaceGoalRepos :many
SELECT r.* FROM goal_repos r JOIN goals g ON g.id = r.goal_id WHERE g.workspace_id = $1;

-- name: UpdateGoalRepo :one
UPDATE goal_repos
SET base_sha = coalesce(sqlc.narg(base_sha), base_sha),
    tip_sha = coalesce(sqlc.narg(tip_sha), tip_sha),
    commits = coalesce(sqlc.narg(commits), commits),
    bundle_key = coalesce(sqlc.narg(bundle_key), bundle_key),
    pr_url = coalesce(sqlc.narg(pr_url), pr_url),
    status = coalesce(sqlc.narg(status), status)
WHERE goal_id = sqlc.arg(goal_id) AND directory_id = sqlc.arg(directory_id)
RETURNING *;

-- name: ListGoalsMovedSince :many
SELECT * FROM goals WHERE workspace_id = $1 AND updated_at >= $2 ORDER BY updated_at DESC LIMIT 20;
