-- name: EnsureMirrorState :one
INSERT INTO mirror_state (directory_id) VALUES ($1)
ON CONFLICT (directory_id) DO UPDATE SET directory_id = excluded.directory_id
RETURNING *;

-- name: GetMirrorState :one
SELECT * FROM mirror_state WHERE directory_id = $1;

-- name: BumpMirrorRevision :one
UPDATE mirror_state
SET revision = revision + 1, last_synced_at = now(),
    complete = complete OR sqlc.arg(is_full)::boolean, missing = sqlc.arg(is_missing)::boolean
WHERE directory_id = sqlc.arg(directory_id)
RETURNING *;

-- name: UpsertMirrorFile :exec
INSERT INTO mirror_files (directory_id, path, sha256, size, mode, revision)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (directory_id, path) DO UPDATE
SET sha256 = excluded.sha256, size = excluded.size, mode = excluded.mode, revision = excluded.revision, updated_at = now();

-- name: DeleteMirrorFile :exec
DELETE FROM mirror_files WHERE directory_id = $1 AND path = $2;

-- name: DeleteMirrorFilesNotIn :exec
DELETE FROM mirror_files WHERE directory_id = $1 AND NOT (path = ANY(sqlc.arg(keep)::text[]));

-- name: ListMirrorFiles :many
SELECT * FROM mirror_files WHERE directory_id = $1 ORDER BY path;

-- name: GetMirrorFile :one
SELECT * FROM mirror_files WHERE directory_id = $1 AND path = $2;

-- name: SearchMirrorFiles :many
SELECT * FROM mirror_files WHERE directory_id = $1 AND path ILIKE '%' || sqlc.arg(q)::text || '%' ORDER BY path LIMIT 200;

-- name: CreatePendingWrite :one
INSERT INTO pending_writes (id, directory_id, path, base_sha, new_sha, mode, member_id, task_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListQueuedWrites :many
SELECT * FROM pending_writes WHERE directory_id = $1 AND status = 'queued' ORDER BY created_at, id;

-- name: SetPendingWriteStatus :one
UPDATE pending_writes
SET status = sqlc.arg(status), current_sha = sqlc.arg(current_sha),
    applied_at = CASE WHEN sqlc.arg(status) = 'applied' THEN now() ELSE applied_at END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetPendingWrite :one
SELECT * FROM pending_writes WHERE id = $1;

-- name: ListDirectoriesOnMachine :many
SELECT d.* FROM directories d WHERE d.machine_id = $1;

-- name: CountQueuedWrites :one
SELECT count(*) FROM pending_writes WHERE directory_id = $1 AND status = 'queued';
