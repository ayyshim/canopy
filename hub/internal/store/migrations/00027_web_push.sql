-- +goose Up
-- The hub's VAPID key pair for web push, made once (like hub_pki).
CREATE TABLE vapid_keys (
  id          boolean PRIMARY KEY DEFAULT true CHECK (id),
  public_key  text NOT NULL,
  private_key text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE push_subscriptions (
  id         uuid PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  endpoint   text NOT NULL UNIQUE,
  p256dh     text NOT NULL,
  auth       text NOT NULL,
  user_agent text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  last_ok_at timestamptz
);
CREATE INDEX push_subscriptions_user ON push_subscriptions (user_id);

-- One-tap actions from a notification. Only the hash of the token is stored.
CREATE TABLE push_action_tokens (
  token_hash bytea PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  signal_id  uuid NOT NULL REFERENCES signals (id) ON DELETE CASCADE,
  actions    text[] NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX push_action_tokens_expires ON push_action_tokens (expires_at);

ALTER TABLE users ADD COLUMN snooze_until timestamptz;

-- +goose Down
ALTER TABLE users DROP COLUMN snooze_until;
DROP TABLE push_action_tokens;
DROP TABLE push_subscriptions;
DROP TABLE vapid_keys;
