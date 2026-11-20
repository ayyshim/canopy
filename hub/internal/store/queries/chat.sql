-- name: CreateChatMessage :one
INSERT INTO chat_messages (id, workspace_id, member_id, author_user_id, kind, text, as_goal, refs)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListChatMessages :many
SELECT * FROM chat_messages WHERE member_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: MarkChatRead :exec
INSERT INTO chat_reads (user_id, member_id, last_read_at) VALUES ($1, $2, now())
ON CONFLICT (user_id, member_id) DO UPDATE SET last_read_at = now();

-- name: UnreadCounts :many
SELECT m.member_id, count(*)::bigint AS unread
FROM chat_messages m
LEFT JOIN chat_reads r ON r.member_id = m.member_id AND r.user_id = $2
WHERE m.workspace_id = $1 AND m.kind <> 'human' AND (r.last_read_at IS NULL OR m.created_at > r.last_read_at)
GROUP BY m.member_id;

-- name: CreateComment :one
INSERT INTO comments (id, task_id, author_user_id, author_member_id, text)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListComments :many
SELECT * FROM comments WHERE task_id = $1 ORDER BY created_at, id;
