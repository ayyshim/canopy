-- name: ListRunnerConnections :many
SELECT * FROM cli_connections WHERE runner_id = $1 ORDER BY cli;

-- name: ListOrgConnections :many
SELECT * FROM cli_connections WHERE org_id = $1 ORDER BY runner_id, cli;

-- name: GetCliConnection :one
SELECT * FROM cli_connections WHERE runner_id = $1 AND cli = $2;

-- name: UpsertCliConnection :one
INSERT INTO cli_connections (id, org_id, runner_id, cli, mode, cap_tokens_per_day, key_fingerprint, enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (runner_id, cli) DO UPDATE
SET mode = excluded.mode, cap_tokens_per_day = excluded.cap_tokens_per_day,
    key_fingerprint = CASE WHEN excluded.key_fingerprint = '' THEN cli_connections.key_fingerprint ELSE excluded.key_fingerprint END,
    enabled = excluded.enabled
RETURNING *;

-- name: EnsureAccount :one
INSERT INTO accounts (id, org_id, runner_id, cli, mode)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (runner_id, cli) DO UPDATE SET mode = excluded.mode
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE runner_id = $1 AND cli = $2;

-- name: ListAccounts :many
SELECT * FROM accounts WHERE org_id = $1 ORDER BY runner_id, cli;
