-- name: CreateNote :one
INSERT INTO notes (id, workspace_id, goal_id, kind, title, body, author_user_id, author_member_id, number,
  visibility, color, board_x, board_y, job_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, next_item_number($2, 'note'),
  coalesce(sqlc.narg(visibility), 'shared'), coalesce(sqlc.narg(color), 'yellow'), sqlc.narg(board_x), sqlc.narg(board_y), sqlc.narg(job_id))
RETURNING *;

-- name: GetNote :one
SELECT * FROM notes WHERE id = $1;

-- name: ListNotes :many
-- Shared notes, plus the viewer's own private ones. No viewer means shared notes only.
SELECT * FROM notes
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (visibility = 'shared' OR author_user_id = sqlc.narg(viewer_id))
ORDER BY updated_at DESC, id;

-- name: UpdateNote :one
UPDATE notes SET title = coalesce(sqlc.narg(title), title), body = coalesce(sqlc.narg(body), body),
  kind = coalesce(sqlc.narg(kind), kind),
  visibility = coalesce(sqlc.narg(visibility), visibility), color = coalesce(sqlc.narg(color), color),
  board_x = CASE WHEN sqlc.arg(set_pos)::boolean THEN sqlc.narg(board_x) ELSE board_x END,
  board_y = CASE WHEN sqlc.arg(set_pos)::boolean THEN sqlc.narg(board_y) ELSE board_y END,
  edited_by_user_id = CASE WHEN sqlc.arg(set_editor)::boolean THEN sqlc.narg(edited_by_user_id) ELSE edited_by_user_id END,
  edited_by_member_id = CASE WHEN sqlc.arg(set_editor)::boolean THEN sqlc.narg(edited_by_member_id) ELSE edited_by_member_id END
WHERE id = sqlc.arg(id) RETURNING *;

-- name: DeleteNote :exec
DELETE FROM notes WHERE id = $1;

-- name: GoalNote :one
SELECT * FROM notes WHERE goal_id = $1 AND kind = $2 ORDER BY created_at DESC LIMIT 1;

-- name: GetNoteByNumber :one
SELECT * FROM notes WHERE workspace_id = $1 AND number = $2;

-- name: AddNoteRevision :one
INSERT INTO note_revisions (id, note_id, rev, title, body, kind, edited_by_user_id, edited_by_member_id)
SELECT sqlc.arg(id), n.id, coalesce((SELECT max(x.rev) FROM note_revisions x WHERE x.note_id = n.id), 0) + 1,
  n.title, n.body, n.kind, n.edited_by_user_id, n.edited_by_member_id
FROM notes n WHERE n.id = sqlc.arg(for_note)
RETURNING *;

-- name: PruneNoteRevisions :exec
DELETE FROM note_revisions r WHERE r.note_id = $1
  AND r.rev <= (SELECT max(x.rev) FROM note_revisions x WHERE x.note_id = $1) - 20;

-- name: ListNoteRevisions :many
SELECT * FROM note_revisions WHERE note_id = $1 ORDER BY rev DESC;

-- name: GetNoteRevision :one
SELECT * FROM note_revisions WHERE note_id = $1 AND rev = $2;
