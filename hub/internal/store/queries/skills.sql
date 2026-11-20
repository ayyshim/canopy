-- name: CreateWorkspaceSkill :one
INSERT INTO workspace_skills (id, workspace_id, name, source_kind, source_url, content)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetWorkspaceSkill :one
SELECT * FROM workspace_skills WHERE id = $1;

-- name: ListWorkspaceSkills :many
SELECT * FROM workspace_skills WHERE workspace_id = $1 ORDER BY lower(name), id;

-- name: UpdateWorkspaceSkill :one
UPDATE workspace_skills
SET name = coalesce(sqlc.narg(name), name),
    source_kind = coalesce(sqlc.narg(source_kind), source_kind),
    source_url = coalesce(sqlc.narg(source_url), source_url),
    content = coalesce(sqlc.narg(content), content),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteWorkspaceSkill :exec
DELETE FROM workspace_skills WHERE id = $1;

-- name: AttachMemberSkill :exec
INSERT INTO member_skills (member_id, skill_id) VALUES ($1, $2)
ON CONFLICT (member_id, skill_id) DO NOTHING;

-- name: DetachMemberSkill :exec
DELETE FROM member_skills WHERE member_id = $1 AND skill_id = $2;

-- name: ListMemberSkills :many
SELECT s.* FROM workspace_skills s JOIN member_skills ms ON ms.skill_id = s.id
WHERE ms.member_id = $1
ORDER BY lower(s.name), s.id;

-- name: ListWorkspaceMemberSkills :many
SELECT ms.member_id, ms.skill_id FROM member_skills ms JOIN members m ON m.id = ms.member_id
WHERE m.workspace_id = $1
ORDER BY ms.member_id, ms.created_at, ms.skill_id;
