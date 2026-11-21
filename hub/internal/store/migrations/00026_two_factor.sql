-- +goose Up
ALTER TABLE users
  ADD COLUMN totp_secret text,
  ADD COLUMN totp_enabled_at timestamptz,
  ADD COLUMN totp_last_step bigint;

-- A null two_factor_at means the session still needs a code, so sessions that exist when this
-- migration runs are pending and their owners enroll on the next request.
ALTER TABLE auth_sessions
  ADD COLUMN two_factor_at timestamptz,
  ADD COLUMN two_factor_failures int NOT NULL DEFAULT 0;

CREATE TABLE user_recovery_codes (
  id         uuid PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  code_hash  bytea NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, code_hash)
);

-- +goose Down
DROP TABLE user_recovery_codes;
ALTER TABLE auth_sessions DROP COLUMN two_factor_failures, DROP COLUMN two_factor_at;
ALTER TABLE users DROP COLUMN totp_last_step, DROP COLUMN totp_enabled_at, DROP COLUMN totp_secret;
