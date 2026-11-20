-- name: CreateStaffProposal :one
INSERT INTO staff_proposals (id, workspace_id, manager_id, kind, member_id, name, role, instructions, skills, session_id, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetStaffProposalForUpdate :one
SELECT * FROM staff_proposals WHERE id = $1 FOR UPDATE;

-- name: ListStaffProposals :many
SELECT * FROM staff_proposals
WHERE workspace_id = $1 AND (sqlc.arg(status)::text = '' OR status = sqlc.arg(status)::text)
ORDER BY created_at DESC, id;

-- name: DecideStaffProposal :one
UPDATE staff_proposals
SET status = sqlc.arg(status), decided_by = sqlc.arg(decided_by), decided_at = now(), reason = sqlc.arg(reason),
    name = sqlc.arg(name), role = sqlc.narg(role), instructions = sqlc.arg(instructions), skills = sqlc.arg(skills),
    result_member_id = sqlc.narg(result_member_id)
WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING *;

-- name: GetWorkspaceSkillByName :one
SELECT * FROM workspace_skills WHERE workspace_id = $1 AND lower(name) = lower($2);
