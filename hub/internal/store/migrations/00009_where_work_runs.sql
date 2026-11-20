-- +goose Up
-- DECISIONS: "Where work runs", "Code in git repositories", "Workspace knowledge".

-- A shared computer (signed in with an API key) runs goals for people without their own.
ALTER TABLE runners ADD COLUMN shared boolean NOT NULL DEFAULT false;

-- Each person's computer for their goals: the first they pair, changeable.
ALTER TABLE memberships ADD COLUMN default_runner_id uuid REFERENCES runners (id) ON DELETE SET NULL;
UPDATE memberships m SET default_runner_id = (
  SELECT r.id FROM runners r
  WHERE r.org_id = m.org_id AND r.owner_user_id = m.user_id AND r.revoked_at IS NULL
  ORDER BY r.created_at LIMIT 1);

-- No workspace folder or workspace runner: knowledge lives in the hub.
ALTER TABLE workspaces DROP COLUMN folder_path, DROP COLUMN runner_id;
ALTER TABLE workspaces ADD COLUMN goal_completion text NOT NULL DEFAULT 'manual'
  CHECK (goal_completion IN ('manual', 'auto'));

CREATE TABLE notes (
  id               uuid PRIMARY KEY,
  workspace_id     uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  goal_id          uuid REFERENCES goals (id) ON DELETE SET NULL,
  kind             text NOT NULL CHECK (kind IN ('note', 'plan', 'decision', 'summary')),
  title            text NOT NULL,
  body             text NOT NULL DEFAULT '',
  author_user_id   uuid REFERENCES users (id) ON DELETE SET NULL,
  author_member_id uuid REFERENCES members (id) ON DELETE SET NULL,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notes_workspace ON notes (workspace_id, updated_at DESC);
CREATE TRIGGER notes_updated_at BEFORE UPDATE ON notes FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Repositories are shared through git; folders keep the hub copy.
ALTER TABLE directories ADD COLUMN repo_remote text NOT NULL DEFAULT '';
ALTER TABLE directories ADD COLUMN default_branch text NOT NULL DEFAULT '';
ALTER TABLE directories ADD COLUMN setup_command text NOT NULL DEFAULT '';
ALTER TABLE directories ADD COLUMN copy_files text[] NOT NULL DEFAULT '{}';

-- Each person's own clone of a repository, per computer.
CREATE TABLE directory_clones (
  id             uuid PRIMARY KEY,
  directory_id   uuid NOT NULL REFERENCES directories (id) ON DELETE CASCADE,
  runner_id      uuid NOT NULL REFERENCES runners (id) ON DELETE CASCADE,
  user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  path           text NOT NULL,
  current_branch text NOT NULL DEFAULT '',
  dirty          integer NOT NULL DEFAULT 0,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (directory_id, runner_id)
);
CREATE TRIGGER directory_clones_updated_at BEFORE UPDATE ON directory_clones FOR EACH ROW EXECUTE FUNCTION set_updated_at();
INSERT INTO directory_clones (id, directory_id, runner_id, user_id, path)
SELECT gen_random_uuid(), d.id, d.machine_id, d.owner_user_id, d.path FROM directories d WHERE d.machine_id IS NOT NULL;

-- Goals: where they run, their branch, and the ready → shipped steps.
ALTER TABLE goals DROP CONSTRAINT goals_status_check;
ALTER TABLE goals ADD CONSTRAINT goals_status_check CHECK (status IN ('planning', 'active', 'ready', 'done'));
ALTER TABLE goals ADD COLUMN runner_id uuid REFERENCES runners (id) ON DELETE SET NULL;
ALTER TABLE goals ADD COLUMN base text NOT NULL DEFAULT '';
ALTER TABLE goals ADD COLUMN branch text NOT NULL DEFAULT '';
ALTER TABLE goals ADD COLUMN snapshot boolean NOT NULL DEFAULT false;
ALTER TABLE goals ADD COLUMN shipped jsonb NOT NULL DEFAULT '{}';
ALTER TABLE goals ADD COLUMN show_tasks boolean NOT NULL DEFAULT false;
ALTER TABLE goals ADD COLUMN done_by uuid REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE goals ADD COLUMN ready_at timestamptz;

-- One row per repository a goal changes: its branch's state and the hub's backup of it.
CREATE TABLE goal_repos (
  goal_id      uuid NOT NULL REFERENCES goals (id) ON DELETE CASCADE,
  directory_id uuid NOT NULL REFERENCES directories (id) ON DELETE CASCADE,
  base         text NOT NULL DEFAULT '',
  base_sha     text NOT NULL DEFAULT '',
  tip_sha      text NOT NULL DEFAULT '',
  commits      integer NOT NULL DEFAULT 0,
  bundle_key   text NOT NULL DEFAULT '',
  pr_url       text NOT NULL DEFAULT '',
  status       text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (goal_id, directory_id)
);
CREATE TRIGGER goal_repos_updated_at BEFORE UPDATE ON goal_repos FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Sessions belong to a goal, or to a person (chat and work without a goal).
ALTER TABLE sessions ADD COLUMN goal_id uuid REFERENCES goals (id) ON DELETE CASCADE;
ALTER TABLE sessions ADD COLUMN person_id uuid REFERENCES users (id) ON DELETE CASCADE;
CREATE INDEX sessions_key ON sessions (member_id, goal_id, person_id, number DESC);

-- Signals remember the session waiting on them and, for account cards, whom they're for.
ALTER TABLE signals ADD COLUMN session_id uuid REFERENCES sessions (id) ON DELETE SET NULL;
ALTER TABLE signals ADD COLUMN for_user_id uuid REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE signals ADD COLUMN goal_id uuid REFERENCES goals (id) ON DELETE CASCADE;
ALTER TABLE signals DROP CONSTRAINT signals_type_check;
ALTER TABLE signals ADD CONSTRAINT signals_type_check CHECK (type IN ('question', 'gate', 'dir_access', 'error', 'review',
  'usage_limit', 'cap', 'owner_change', 'sync_conflict', 'goal_ready', 'git'));

-- Sign-ins belong to a computer, not the organization.
ALTER TABLE cli_connections ADD COLUMN runner_id uuid REFERENCES runners (id) ON DELETE CASCADE;
INSERT INTO cli_connections (id, org_id, runner_id, cli, mode, cap_tokens_per_day, key_fingerprint, enabled)
SELECT gen_random_uuid(), c.org_id, r.id, c.cli, c.mode, c.cap_tokens_per_day, c.key_fingerprint, c.enabled
FROM cli_connections c JOIN runners r ON r.org_id = c.org_id AND r.revoked_at IS NULL
WHERE c.runner_id IS NULL;
DELETE FROM cli_connections WHERE runner_id IS NULL;
ALTER TABLE cli_connections ALTER COLUMN runner_id SET NOT NULL;
ALTER TABLE cli_connections DROP CONSTRAINT cli_connections_org_id_cli_key;
ALTER TABLE cli_connections ADD CONSTRAINT cli_connections_runner_cli UNIQUE (runner_id, cli);

DELETE FROM accounts;
ALTER TABLE accounts ADD COLUMN runner_id uuid NOT NULL REFERENCES runners (id) ON DELETE CASCADE;
ALTER TABLE accounts DROP CONSTRAINT accounts_org_id_cli_key;
ALTER TABLE accounts ADD CONSTRAINT accounts_runner_cli UNIQUE (runner_id, cli);

-- +goose Down
DELETE FROM accounts;
ALTER TABLE accounts DROP CONSTRAINT accounts_runner_cli;
ALTER TABLE accounts DROP COLUMN runner_id;
ALTER TABLE accounts ADD CONSTRAINT accounts_org_id_cli_key UNIQUE (org_id, cli);
DELETE FROM cli_connections c USING cli_connections d
  WHERE c.org_id = d.org_id AND c.cli = d.cli AND c.id > d.id;
ALTER TABLE cli_connections DROP CONSTRAINT cli_connections_runner_cli;
ALTER TABLE cli_connections DROP COLUMN runner_id;
ALTER TABLE cli_connections ADD CONSTRAINT cli_connections_org_id_cli_key UNIQUE (org_id, cli);
DELETE FROM signals WHERE type IN ('goal_ready', 'git');
ALTER TABLE signals DROP CONSTRAINT signals_type_check;
ALTER TABLE signals ADD CONSTRAINT signals_type_check CHECK (type IN ('question', 'gate', 'dir_access', 'error', 'review',
  'usage_limit', 'cap', 'owner_change', 'sync_conflict'));
ALTER TABLE signals DROP COLUMN goal_id, DROP COLUMN for_user_id, DROP COLUMN session_id;
DROP INDEX sessions_key;
ALTER TABLE sessions DROP COLUMN person_id, DROP COLUMN goal_id;
DROP TABLE goal_repos;
UPDATE goals SET status = 'done' WHERE status = 'ready';
ALTER TABLE goals DROP CONSTRAINT goals_status_check;
ALTER TABLE goals ADD CONSTRAINT goals_status_check CHECK (status IN ('planning', 'active', 'done'));
ALTER TABLE goals DROP COLUMN ready_at, DROP COLUMN done_by, DROP COLUMN show_tasks, DROP COLUMN shipped,
  DROP COLUMN snapshot, DROP COLUMN branch, DROP COLUMN base, DROP COLUMN runner_id;
DROP TABLE directory_clones;
ALTER TABLE directories DROP COLUMN copy_files, DROP COLUMN setup_command, DROP COLUMN default_branch, DROP COLUMN repo_remote;
DROP TABLE notes;
ALTER TABLE workspaces DROP COLUMN goal_completion;
ALTER TABLE workspaces ADD COLUMN folder_path text NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN runner_id uuid REFERENCES runners (id) ON DELETE SET NULL;
ALTER TABLE memberships DROP COLUMN default_runner_id;
ALTER TABLE runners DROP COLUMN shared;
