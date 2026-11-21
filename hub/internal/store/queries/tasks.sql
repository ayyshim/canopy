-- name: CreateTask :one
INSERT INTO tasks (id, workspace_id, goal_id, member_id, kind, title, description, skill, status, depends_on, steps,
                   required_dirs, borrowed, attributed_user_id, created_by, created_by_member, parent_task_id, plan_key, number)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, next_item_number($2, 'task'))
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE id = $1;

-- name: GetTaskForUpdate :one
SELECT * FROM tasks WHERE id = $1 FOR UPDATE;

-- name: ListTasks :many
SELECT * FROM tasks WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: ListGoalTasks :many
SELECT * FROM tasks WHERE goal_id = $1 ORDER BY created_at, id;

-- name: ListMemberOpenTasks :many
SELECT * FROM tasks WHERE member_id = $1 AND status <> 'done' ORDER BY created_at, id;

-- name: UpdateTask :one
UPDATE tasks
SET status = coalesce(sqlc.narg(status), status),
    progress = coalesce(sqlc.narg(progress), progress),
    current_step = coalesce(sqlc.narg(current_step), current_step),
    steps = coalesce(sqlc.narg(steps), steps),
    summary = coalesce(sqlc.narg(summary), summary),
    member_id = CASE WHEN sqlc.arg(set_member)::boolean THEN sqlc.narg(member_id) ELSE member_id END,
    borrowed = coalesce(sqlc.narg(borrowed), borrowed),
    depends_on = coalesce(sqlc.narg(depends_on), depends_on),
    started_at = CASE WHEN sqlc.arg(set_started)::boolean THEN now() ELSE started_at END,
    finished_at = CASE WHEN sqlc.arg(set_finished)::boolean THEN now() ELSE finished_at END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = $1;

-- name: LockWorkspace :exec
-- Serializes scheduling decisions for one workspace within a transaction.
SELECT pg_advisory_xact_lock(hashtext('canopy.ws.' || sqlc.arg(workspace_id)::uuid::text));

-- name: ListActiveWorkspaces :many
SELECT DISTINCT workspace_id FROM tasks WHERE status <> 'done';

-- name: BumpTaskNudges :one
UPDATE tasks SET nudges = nudges + 1 WHERE id = $1 RETURNING nudges;

-- name: ResetTaskNudges :exec
UPDATE tasks SET nudges = 0 WHERE id = $1;

-- name: SetTaskDirs :one
UPDATE tasks SET required_dirs = $2 WHERE id = $1 RETURNING *;
