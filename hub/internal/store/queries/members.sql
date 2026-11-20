-- name: CreateMember :one
INSERT INTO members (id, workspace_id, name, kind, skill, reports_to, cli, model, instructions, avatar_color, all_directories, position)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetMember :one
SELECT * FROM members WHERE id = $1;

-- name: ListMembers :many
SELECT * FROM members WHERE workspace_id = $1 ORDER BY position, created_at, id;

-- name: UpdateMember :one
UPDATE members
SET name = coalesce(sqlc.narg(name), name),
    skill = CASE WHEN sqlc.arg(set_skill)::boolean THEN sqlc.narg(skill) ELSE skill END,
    reports_to = CASE WHEN sqlc.arg(set_reports_to)::boolean THEN sqlc.narg(reports_to) ELSE reports_to END,
    cli = coalesce(sqlc.narg(cli), cli),
    model = coalesce(sqlc.narg(model), model),
    instructions = coalesce(sqlc.narg(instructions), instructions),
    all_directories = coalesce(sqlc.narg(all_directories), all_directories),
    sleeping = coalesce(sqlc.narg(sleeping), sleeping)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteMember :exec
DELETE FROM members WHERE id = $1;

-- name: ReassignStaff :exec
UPDATE members SET reports_to = sqlc.narg(new_manager) WHERE reports_to = sqlc.arg(old_manager);

-- name: CountManagers :one
SELECT count(*) FROM members WHERE workspace_id = $1 AND kind = 'manager';

-- name: ListGrants :many
SELECT g.* FROM directory_grants g JOIN members m ON m.id = g.member_id
WHERE m.workspace_id = $1
ORDER BY g.member_id, g.directory_id;

-- name: ListMemberGrants :many
SELECT * FROM directory_grants WHERE member_id = $1 ORDER BY directory_id;

-- name: UpsertGrant :one
INSERT INTO directory_grants (id, member_id, directory_id, mode, source, reason, granted_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (member_id, directory_id) DO UPDATE
SET mode = excluded.mode, source = excluded.source, reason = excluded.reason, granted_by = excluded.granted_by
RETURNING *;

-- name: DeleteGrant :exec
DELETE FROM directory_grants WHERE member_id = $1 AND directory_id = $2;

-- name: DeleteMemberGrants :exec
DELETE FROM directory_grants WHERE member_id = $1;
