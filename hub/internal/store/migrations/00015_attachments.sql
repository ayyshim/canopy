-- +goose Up
CREATE TABLE attachments (
  id           uuid PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  goal_id      uuid REFERENCES goals (id) ON DELETE CASCADE,
  task_id      uuid REFERENCES tasks (id) ON DELETE CASCADE,
  name         text NOT NULL,
  mime         text NOT NULL,
  size         bigint NOT NULL,
  sha256       text NOT NULL,
  user_id      uuid REFERENCES users (id) ON DELETE SET NULL,
  member_id    uuid REFERENCES members (id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CHECK ((goal_id IS NULL) <> (task_id IS NULL))
);
CREATE INDEX attachments_workspace ON attachments (workspace_id);
CREATE INDEX attachments_goal ON attachments (goal_id);
CREATE INDEX attachments_task ON attachments (task_id);

-- +goose Down
DROP TABLE attachments;
