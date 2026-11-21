-- name: SetUserTotpSecret :execrows
-- Only while the person has not finished enrolling.
UPDATE users SET totp_secret = $2 WHERE id = $1 AND totp_enabled_at IS NULL;

-- name: EnableUserTotp :execrows
UPDATE users SET totp_enabled_at = now(), totp_last_step = $2
WHERE id = $1 AND totp_enabled_at IS NULL AND totp_secret IS NOT NULL;

-- name: ClaimTotpStep :execrows
-- Succeeds once per time step, so a code can't be replayed.
UPDATE users SET totp_last_step = $2
WHERE id = $1 AND totp_enabled_at IS NOT NULL AND (totp_last_step IS NULL OR totp_last_step < $2);

-- name: InsertRecoveryCode :exec
INSERT INTO user_recovery_codes (id, user_id, code_hash) VALUES ($1, $2, $3);

-- name: ClaimRecoveryCode :execrows
UPDATE user_recovery_codes SET used_at = now()
WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: MarkSessionVerified :exec
UPDATE auth_sessions SET two_factor_at = now(), two_factor_failures = 0 WHERE id = $1;

-- name: AddSessionTwoFactorFailure :one
UPDATE auth_sessions SET two_factor_failures = two_factor_failures + 1 WHERE id = $1
RETURNING two_factor_failures;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL;

-- name: DeleteRecoveryCodes :exec
DELETE FROM user_recovery_codes WHERE user_id = $1;

-- name: ClearUserTotp :exec
UPDATE users SET totp_secret = NULL, totp_enabled_at = NULL, totp_last_step = NULL WHERE id = $1;

-- name: MarkSessionPending :exec
UPDATE auth_sessions SET two_factor_at = NULL, two_factor_failures = 0 WHERE id = $1;
