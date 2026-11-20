-- name: CreatePairing :one
INSERT INTO runner_pairings (id, org_id, user_id, code_hash, name, kind, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ClaimPairing :one
UPDATE runner_pairings SET used_at = now()
WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()
RETURNING *;

-- name: SetPairingRunner :exec
UPDATE runner_pairings SET runner_id = $2 WHERE id = $1;

-- name: GetPairing :one
SELECT * FROM runner_pairings WHERE id = $1;

-- name: CreateRunner :one
INSERT INTO runners (id, org_id, owner_user_id, name, kind, cert_fingerprint, hostname, os, arch, version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetRunner :one
SELECT * FROM runners WHERE id = $1;

-- name: GetRunnerByFingerprint :one
SELECT * FROM runners WHERE cert_fingerprint = $1 AND revoked_at IS NULL;

-- name: ListOrgRunners :many
SELECT * FROM runners WHERE org_id = $1 AND revoked_at IS NULL ORDER BY name, id;

-- name: SetRunnerOnline :one
UPDATE runners
SET online = sqlc.arg(online), last_seen_at = now(),
    hostname = coalesce(sqlc.narg(hostname), hostname), os = coalesce(sqlc.narg(os), os),
    arch = coalesce(sqlc.narg(arch), arch), version = coalesce(sqlc.narg(version), version),
    clis = coalesce(sqlc.narg(clis), clis), sandbox = coalesce(sqlc.narg(sandbox), sandbox)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: TouchRunner :exec
UPDATE runners SET last_seen_at = now() WHERE id = $1;

-- name: RevokeRunner :exec
UPDATE runners SET revoked_at = now(), online = false WHERE id = $1;

-- name: MarkAllRunnersOffline :exec
UPDATE runners SET online = false WHERE online;

-- name: UpdateRunner :one
UPDATE runners SET name = coalesce(sqlc.narg(name), name), shared = coalesce(sqlc.narg(shared), shared)
WHERE id = sqlc.arg(id) RETURNING *;

-- name: ListSharedRunners :many
SELECT * FROM runners WHERE org_id = $1 AND shared AND revoked_at IS NULL ORDER BY created_at;

-- name: SetDefaultRunner :exec
UPDATE memberships SET default_runner_id = sqlc.narg(runner_id) WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id);

-- name: SetDefaultRunnerIfEmpty :exec
UPDATE memberships SET default_runner_id = $3 WHERE org_id = $1 AND user_id = $2 AND default_runner_id IS NULL;

-- name: ListDefaultRunners :many
SELECT user_id, default_runner_id FROM memberships WHERE org_id = $1 AND default_runner_id IS NOT NULL;
