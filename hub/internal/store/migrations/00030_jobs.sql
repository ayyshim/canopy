-- +goose Up
-- DESIGN: docs/design/job-scheduler.md section 4.

CREATE TABLE jobs (
  id                   uuid PRIMARY KEY,
  workspace_id         uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  title                text NOT NULL,
  kind                 text NOT NULL CHECK (kind IN ('single', 'continuous')),
  status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'ended')),
  pause_reason         text NOT NULL DEFAULT '' CHECK (pause_reason IN ('', 'owner', 'failures', 'cap', 'owner_left')),
  brief                text NOT NULL DEFAULT '',
  owner_user_id        uuid REFERENCES users (id) ON DELETE SET NULL,
  manager_id           uuid REFERENCES members (id) ON DELETE SET NULL,
  assignee_member_id   uuid REFERENCES members (id) ON DELETE SET NULL,
  runner_id            uuid REFERENCES runners (id) ON DELETE SET NULL,
  cron                 text NOT NULL,
  timezone             text NOT NULL DEFAULT 'UTC',
  next_run_at          timestamptz,
  last_run_at          timestamptz,
  missed_policy        text NOT NULL DEFAULT 'skip' CHECK (missed_policy IN ('skip', 'run_once')),
  catchup_window_secs  integer NOT NULL DEFAULT 3600,
  run_timeout_secs     integer NOT NULL DEFAULT 7200,
  token_cap_per_run    bigint,
  token_cap_per_month  bigint,
  notify               text NOT NULL DEFAULT 'attention' CHECK (notify IN ('attention', 'always', 'failures')),
  ends_at              timestamptz,
  max_runs             integer,
  required_dirs        jsonb NOT NULL DEFAULT '[]',
  memory_note_id       uuid REFERENCES notes (id) ON DELETE SET NULL,
  consecutive_failures integer NOT NULL DEFAULT 0,
  created_by           uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX jobs_workspace ON jobs (workspace_id, created_at DESC);
CREATE INDEX jobs_due ON jobs (next_run_at) WHERE status = 'active';
CREATE TRIGGER jobs_updated_at BEFORE UPDATE ON jobs FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE job_runs (
  id             uuid PRIMARY KEY,
  job_id         uuid NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
  number         integer NOT NULL,
  scheduled_for  timestamptz NOT NULL,
  trigger        text NOT NULL DEFAULT 'schedule' CHECK (trigger IN ('schedule', 'manual', 'test')),
  status         text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'succeeded', 'failed', 'skipped', 'capped', 'cancelled')),
  skip_reason    text NOT NULL DEFAULT '',
  attempt        integer NOT NULL DEFAULT 1,
  goal_id        uuid REFERENCES goals (id) ON DELETE SET NULL,
  started_at     timestamptz,
  finished_at    timestamptz,
  tokens         bigint NOT NULL DEFAULT 0,
  result         text NOT NULL DEFAULT '' CHECK (result IN ('', 'ok', 'attention', 'nothing_to_do')),
  title          text NOT NULL DEFAULT '',
  summary        text NOT NULL DEFAULT '',
  error          text NOT NULL DEFAULT '',
  pending_memory text,
  memory_after   text,
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (job_id, number)
);
-- A due time starts once; a job never has two running runs.
CREATE UNIQUE INDEX job_runs_due_once ON job_runs (job_id, scheduled_for) WHERE trigger = 'schedule';
CREATE UNIQUE INDEX job_runs_one_running ON job_runs (job_id) WHERE status = 'running';
CREATE INDEX job_runs_recent ON job_runs (job_id, number DESC);

ALTER TABLE goals ADD COLUMN job_run_id uuid REFERENCES job_runs (id) ON DELETE SET NULL;
CREATE INDEX goals_job_run ON goals (job_run_id) WHERE job_run_id IS NOT NULL;

ALTER TABLE notes ADD COLUMN job_id uuid REFERENCES jobs (id) ON DELETE CASCADE;
ALTER TABLE notes DROP CONSTRAINT notes_kind_check;
ALTER TABLE notes ADD CONSTRAINT notes_kind_check CHECK (kind IN ('note', 'plan', 'decision', 'summary', 'job_memory'));

ALTER TABLE goals DROP CONSTRAINT goals_status_check;
ALTER TABLE goals ADD CONSTRAINT goals_status_check CHECK (status IN ('draft', 'planning', 'active', 'ready', 'done', 'stopped'));

-- +goose Down
UPDATE goals SET status = 'done' WHERE status = 'stopped';
ALTER TABLE goals DROP CONSTRAINT goals_status_check;
ALTER TABLE goals ADD CONSTRAINT goals_status_check CHECK (status IN ('draft', 'planning', 'active', 'ready', 'done'));

DELETE FROM notes WHERE kind = 'job_memory';
ALTER TABLE notes DROP CONSTRAINT notes_kind_check;
ALTER TABLE notes ADD CONSTRAINT notes_kind_check CHECK (kind IN ('note', 'plan', 'decision', 'summary'));
ALTER TABLE notes DROP COLUMN job_id;

DROP INDEX goals_job_run;
ALTER TABLE goals DROP COLUMN job_run_id;
DROP TABLE job_runs;
DROP TABLE jobs;
