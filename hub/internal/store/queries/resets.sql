-- name: CreatePasswordReset :exec
INSERT INTO password_resets (id, user_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4);

-- name: ClaimPasswordReset :one
-- Marks a valid, unused reset as used and returns it; no row means invalid, expired or used.
UPDATE password_resets
SET used_at = now()
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
RETURNING *;

-- name: GetUsablePasswordReset :one
SELECT * FROM password_resets WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now();
