-- name: CreateNote :one
INSERT INTO notes (id, workspace_id, goal_id, kind, title, body, author_user_id, author_member_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetNote :one
SELECT * FROM notes WHERE id = $1;

-- name: ListNotes :many
SELECT * FROM notes WHERE workspace_id = $1 ORDER BY updated_at DESC, id;

-- name: UpdateNote :one
UPDATE notes SET title = coalesce(sqlc.narg(title), title), body = coalesce(sqlc.narg(body), body),
  kind = coalesce(sqlc.narg(kind), kind)
WHERE id = sqlc.arg(id) RETURNING *;

-- name: DeleteNote :exec
DELETE FROM notes WHERE id = $1;

-- name: GoalNote :one
SELECT * FROM notes WHERE goal_id = $1 AND kind = $2 ORDER BY created_at DESC LIMIT 1;
