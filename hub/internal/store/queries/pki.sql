-- name: GetHubPKI :one
SELECT * FROM hub_pki WHERE id;

-- name: CreateHubPKI :one
INSERT INTO hub_pki (ca_cert_pem, ca_key_pem) VALUES ($1, $2)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: SetRunnerAddress :exec
UPDATE runners SET address = $2 WHERE id = $1;
