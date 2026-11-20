-- name: GetIdentity :one
SELECT * FROM user_identities WHERE provider = $1 AND subject = $2;

-- name: CreateIdentity :exec
INSERT INTO user_identities (id, user_id, provider, subject, email)
VALUES ($1, $2, $3, $4, $5);

-- name: ListUserProviders :many
SELECT DISTINCT provider FROM user_identities WHERE user_id = $1 ORDER BY provider;
