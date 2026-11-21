-- +goose Up
-- Notifications wait here until the dispatcher sends them (Discord, web push).
CREATE TABLE notification_outbox (
  id              uuid PRIMARY KEY,
  org_id          uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  workspace_id    uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  member_id       uuid REFERENCES members (id) ON DELETE SET NULL,
  kind            text NOT NULL CHECK (kind IN ('questions', 'progress', 'goal_ready', 'errors')),
  title           text NOT NULL,
  body            text NOT NULL DEFAULT '',
  link            text NOT NULL DEFAULT '',
  signal_id       uuid REFERENCES signals (id) ON DELETE SET NULL,
  task_id         uuid REFERENCES tasks (id) ON DELETE SET NULL,
  goal_id         uuid REFERENCES goals (id) ON DELETE SET NULL,
  for_user_id     uuid REFERENCES users (id) ON DELETE CASCADE,
  recipients      uuid[] NOT NULL DEFAULT '{}',
  media           jsonb NOT NULL DEFAULT '{}', -- {output_ids: [], links: [{label, url}]}
  discord_sent_at timestamptz,
  discord_attempts int NOT NULL DEFAULT 0,
  discord_last_error text NOT NULL DEFAULT '',
  discord_next_at timestamptz NOT NULL DEFAULT now(),
  push_sent_at    timestamptz,
  push_attempts   int NOT NULL DEFAULT 0,
  push_last_error text NOT NULL DEFAULT '',
  push_next_at    timestamptz NOT NULL DEFAULT now(),
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_outbox_pending ON notification_outbox (created_at)
  WHERE discord_sent_at IS NULL OR push_sent_at IS NULL;
CREATE INDEX notification_outbox_created ON notification_outbox (created_at);

ALTER TABLE workspaces
  ADD COLUMN discord_webhook     text NOT NULL DEFAULT '',
  ADD COLUMN discord_kinds       jsonb NOT NULL DEFAULT '{"questions": true, "progress": true, "goal_ready": true, "errors": true}',
  ADD COLUMN discord_role_id     text NOT NULL DEFAULT '',
  ADD COLUMN discord_forum       boolean NOT NULL DEFAULT false,
  ADD COLUMN discord_last_ok_at  timestamptz,
  ADD COLUMN discord_last_error  text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE workspaces
  DROP COLUMN discord_webhook, DROP COLUMN discord_kinds, DROP COLUMN discord_role_id,
  DROP COLUMN discord_forum, DROP COLUMN discord_last_ok_at, DROP COLUMN discord_last_error;
DROP TABLE notification_outbox;
