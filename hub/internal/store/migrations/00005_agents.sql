-- +goose Up
CREATE TABLE goals (
  id                 uuid PRIMARY KEY,
  workspace_id       uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  manager_id         uuid REFERENCES members (id) ON DELETE SET NULL,
  title              text NOT NULL,
  done_when          text NOT NULL DEFAULT '',
  color              text NOT NULL,
  status             text NOT NULL DEFAULT 'planning' CHECK (status IN ('planning', 'active', 'done')),
  summary            text NOT NULL DEFAULT '',
  created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
  planned_at         timestamptz,
  finished_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX goals_workspace ON goals (workspace_id);

CREATE TABLE tasks (
  id                 uuid PRIMARY KEY,
  workspace_id       uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  goal_id            uuid REFERENCES goals (id) ON DELETE CASCADE,
  member_id          uuid REFERENCES members (id) ON DELETE SET NULL,
  kind               text NOT NULL DEFAULT 'work' CHECK (kind IN ('plan', 'work', 'owner_change')),
  title              text NOT NULL,
  description        text NOT NULL DEFAULT '',
  skill              text,
  status             text NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'waiting', 'doing', 'review', 'done')),
  progress           integer NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
  depends_on         uuid[] NOT NULL DEFAULT '{}',
  steps              jsonb NOT NULL DEFAULT '[]',   -- [{title, done}]
  current_step       integer NOT NULL DEFAULT 0,
  required_dirs      jsonb NOT NULL DEFAULT '[]',   -- [{directory_id, mode}]
  borrowed           boolean NOT NULL DEFAULT false, -- independent staff helping a manager
  attributed_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
  created_by_member  uuid REFERENCES members (id) ON DELETE SET NULL,
  parent_task_id     uuid REFERENCES tasks (id) ON DELETE SET NULL, -- owner_change: the task that asked
  summary            text NOT NULL DEFAULT '',
  plan_key           text NOT NULL DEFAULT '',    -- the manager's id for the task in its plan
  started_at         timestamptz,
  finished_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tasks_workspace ON tasks (workspace_id);
CREATE INDEX tasks_member ON tasks (member_id, status);

CREATE TABLE sessions (
  id             uuid PRIMARY KEY,
  workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  member_id      uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
  number         integer NOT NULL,
  status         text NOT NULL CHECK (status IN ('starting', 'running', 'waiting', 'gate', 'review', 'asleep', 'crashed', 'error', 'ended')),
  runner_id      uuid REFERENCES runners (id) ON DELETE SET NULL,
  cli_session_id text NOT NULL DEFAULT '',
  task_id        uuid REFERENCES tasks (id) ON DELETE SET NULL,
  tokens_in      bigint NOT NULL DEFAULT 0,
  tokens_out     bigint NOT NULL DEFAULT 0,
  recovered_from uuid REFERENCES sessions (id) ON DELETE SET NULL,
  restarts       integer NOT NULL DEFAULT 0,
  started_at     timestamptz NOT NULL DEFAULT now(),
  last_active_at timestamptz NOT NULL DEFAULT now(),
  ended_at       timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (member_id, number)
);

CREATE TABLE log_lines (
  id           bigserial PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  session_id   uuid REFERENCES sessions (id) ON DELETE CASCADE,
  member_id    uuid REFERENCES members (id) ON DELETE CASCADE,
  task_id      uuid REFERENCES tasks (id) ON DELETE SET NULL,
  kind         text NOT NULL CHECK (kind IN ('agent', 'human', 'ask', 'error', 'tool', 'system')),
  text         text NOT NULL,
  at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX log_lines_member ON log_lines (member_id, id DESC);
CREATE INDEX log_lines_task ON log_lines (task_id, id);

CREATE TABLE signals (
  id           uuid PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  type         text NOT NULL CHECK (type IN ('question', 'gate', 'dir_access', 'error', 'review', 'usage_limit', 'cap', 'owner_change', 'sync_conflict')),
  member_id    uuid REFERENCES members (id) ON DELETE CASCADE,
  task_id      uuid REFERENCES tasks (id) ON DELETE CASCADE,
  payload      jsonb NOT NULL DEFAULT '{}',
  status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'timed_out')),
  resolved_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  resolution   jsonb,
  request_id   text NOT NULL DEFAULT '', -- the runner's pending tool call waiting on this
  expires_at   timestamptz,
  resolved_at  timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX signals_open ON signals (workspace_id) WHERE status = 'open';
CREATE INDEX signals_expiry ON signals (expires_at) WHERE status = 'open' AND expires_at IS NOT NULL;

-- "Always" answers to approval gates, per member and action kind.
CREATE TABLE gate_rules (
  id          uuid PRIMARY KEY,
  member_id   uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
  action_kind text NOT NULL,
  created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (member_id, action_kind)
);

CREATE TABLE outputs (
  id           uuid PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  task_id      uuid REFERENCES tasks (id) ON DELETE CASCADE,
  member_id    uuid REFERENCES members (id) ON DELETE SET NULL,
  kind         text NOT NULL CHECK (kind IN ('image', 'recording', 'document', 'diff', 'report')),
  name         text NOT NULL,  -- file name: later writes with the same name are new versions
  title        text NOT NULL,
  status       text NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'review', 'changes', 'approved')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (task_id, name)
);

CREATE TABLE output_versions (
  id         uuid PRIMARY KEY,
  output_id  uuid NOT NULL REFERENCES outputs (id) ON DELETE CASCADE,
  number     integer NOT NULL,
  blob_key   text NOT NULL,
  mime       text NOT NULL,
  size       bigint NOT NULL,
  note       text NOT NULL DEFAULT '',
  session_id uuid REFERENCES sessions (id) ON DELETE SET NULL,
  model      text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (output_id, number)
);

CREATE TABLE output_views (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  output_id  uuid NOT NULL REFERENCES outputs (id) ON DELETE CASCADE,
  version    integer NOT NULL,
  PRIMARY KEY (user_id, output_id)
);

CREATE TABLE chat_messages (
  id               uuid PRIMARY KEY,
  workspace_id     uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  member_id        uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
  author_user_id   uuid REFERENCES users (id) ON DELETE SET NULL,
  kind             text NOT NULL CHECK (kind IN ('human', 'agent', 'event')),
  text             text NOT NULL,
  as_goal          boolean NOT NULL DEFAULT false,
  refs             jsonb NOT NULL DEFAULT '{}',
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chat_messages_member ON chat_messages (member_id, created_at);

CREATE TABLE chat_reads (
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  member_id    uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
  last_read_at timestamptz NOT NULL,
  PRIMARY KEY (user_id, member_id)
);

CREATE TABLE comments (
  id               uuid PRIMARY KEY,
  task_id          uuid NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
  author_user_id   uuid REFERENCES users (id) ON DELETE SET NULL,
  author_member_id uuid REFERENCES members (id) ON DELETE SET NULL,
  text             text NOT NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX comments_task ON comments (task_id, created_at);

CREATE TABLE goal_events (
  id         uuid PRIMARY KEY,
  goal_id    uuid NOT NULL REFERENCES goals (id) ON DELETE CASCADE,
  type       text NOT NULL,
  summary    text NOT NULL,
  refs       jsonb NOT NULL DEFAULT '{}',
  at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX goal_events_goal ON goal_events (goal_id, at);

-- One row per model call reported by a runner (SPEC "Data pipeline").
CREATE TABLE usage_events (
  id                 bigserial PRIMARY KEY,
  org_id             uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  workspace_id       uuid REFERENCES workspaces (id) ON DELETE SET NULL,
  member_id          uuid REFERENCES members (id) ON DELETE SET NULL,
  attributed_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  session_id         uuid REFERENCES sessions (id) ON DELETE SET NULL,
  cli                text NOT NULL,
  model              text NOT NULL,
  tokens_in          bigint NOT NULL,
  tokens_out         bigint NOT NULL,
  at                 timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX usage_events_org_at ON usage_events (org_id, at);
CREATE INDEX usage_events_user_at ON usage_events (attributed_user_id, at);

CREATE TABLE usage_hourly (
  org_id       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  hour         timestamptz NOT NULL,
  user_id      uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  member_id    uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  workspace_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  model        text NOT NULL,
  tokens_in    bigint NOT NULL DEFAULT 0,
  tokens_out   bigint NOT NULL DEFAULT 0,
  calls        bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (org_id, hour, user_id, member_id, workspace_id, model)
);

-- Work from a person over their Canopy limit waits here (SPEC "Per-person limits").
CREATE TABLE queued_work (
  id           uuid PRIMARY KEY,
  org_id       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  kind         text NOT NULL CHECK (kind IN ('goal', 'chat')),
  payload      jsonb NOT NULL,
  reason       text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER goals_updated_at BEFORE UPDATE ON goals FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER tasks_updated_at BEFORE UPDATE ON tasks FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER sessions_updated_at BEFORE UPDATE ON sessions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER signals_updated_at BEFORE UPDATE ON signals FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER gate_rules_updated_at BEFORE UPDATE ON gate_rules FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER outputs_updated_at BEFORE UPDATE ON outputs FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER output_versions_updated_at BEFORE UPDATE ON output_versions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER chat_messages_updated_at BEFORE UPDATE ON chat_messages FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER comments_updated_at BEFORE UPDATE ON comments FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE queued_work, usage_hourly, usage_events, goal_events, comments, chat_reads, chat_messages,
  output_views, output_versions, outputs, gate_rules, signals, log_lines, sessions, tasks, goals;
