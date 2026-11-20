-- name: CreateInvite :one
INSERT INTO invites (id, org_id, email, org_role, token_hash, invited_by, expires_at, workspace_grants)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetInviteByToken :one
SELECT * FROM invites WHERE token_hash = $1;

-- name: GetInvite :one
SELECT * FROM invites WHERE id = $1;

-- name: ListOpenInvites :many
SELECT i.*, u.name AS inviter_name FROM invites i LEFT JOIN users u ON u.id = i.invited_by
WHERE i.org_id = $1 AND i.accepted_at IS NULL AND i.declined_at IS NULL AND i.revoked_at IS NULL
ORDER BY i.created_at DESC;

-- name: MarkInviteAccepted :one
UPDATE invites SET accepted_at = now()
WHERE id = $1 AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL AND expires_at > now()
RETURNING *;

-- name: MarkInviteDeclined :one
UPDATE invites SET declined_at = now()
WHERE id = $1 AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL
RETURNING *;

-- name: RevokeInvite :exec
UPDATE invites SET revoked_at = now() WHERE id = $1 AND org_id = $2 AND accepted_at IS NULL;

-- name: OpenInviteForEmail :one
SELECT EXISTS (
  SELECT 1 FROM invites WHERE org_id = $1 AND email = $2
    AND accepted_at IS NULL AND declined_at IS NULL AND revoked_at IS NULL AND expires_at > now()
);
