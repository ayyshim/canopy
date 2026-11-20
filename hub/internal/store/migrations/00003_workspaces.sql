-- +goose Up
-- Machines running the `canopy` client: runners (where agents work) and directory owners.
CREATE TABLE runners (
  id               uuid PRIMARY KEY,
  org_id           uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  owner_user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name             text NOT NULL,
  kind             text NOT NULL DEFAULT 'computer' CHECK (kind IN ('computer', 'server')),
  cert_fingerprint text UNIQUE,
  hostname         text NOT NULL DEFAULT '',
  os               text NOT NULL DEFAULT '',
  arch             text NOT NULL DEFAULT '',
  version          text NOT NULL DEFAULT '',
  clis             jsonb NOT NULL DEFAULT '[]',   -- detected CLIs: [{cli, installed, version}]
  sandbox          text NOT NULL DEFAULT '',      -- sandbox the runner will use: bwrap, docker, none
  online           boolean NOT NULL DEFAULT false,
  last_seen_at     timestamptz,
  revoked_at       timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX runners_org ON runners (org_id);

CREATE TABLE runner_pairings (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  code_hash  bytea NOT NULL UNIQUE,
  name       text NOT NULL DEFAULT '',
  kind       text NOT NULL DEFAULT 'computer' CHECK (kind IN ('computer', 'server')),
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  runner_id  uuid REFERENCES runners (id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workspaces (
  id          uuid PRIMARY KEY,
  org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  name        text NOT NULL,
  about       text NOT NULL DEFAULT '',
  folder_path text NOT NULL,
  runner_id   uuid REFERENCES runners (id) ON DELETE SET NULL,
  created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workspaces_org ON workspaces (org_id);

-- What a person can see in a workspace. Org owners and admins see everything without a row.
CREATE TABLE workspace_access (
  id                uuid PRIMARY KEY,
  workspace_id      uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  user_id           uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role              text NOT NULL CHECK (role IN ('admin', 'staff')),
  manager_ids       uuid[] NOT NULL DEFAULT '{}',
  independent_staff boolean NOT NULL DEFAULT false,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, user_id)
);
CREATE INDEX workspace_access_user ON workspace_access (user_id);

-- A folder shared from a person's machine; that machine is its source of truth.
CREATE TABLE directories (
  id            uuid PRIMARY KEY,
  workspace_id  uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  path          text NOT NULL,
  name          text NOT NULL,
  kind          text NOT NULL DEFAULT 'code' CHECK (kind IN ('code', 'docs', 'design', 'other')),
  owner_user_id uuid NOT NULL REFERENCES users (id),
  machine_id    uuid REFERENCES runners (id) ON DELETE SET NULL, -- null until the owner's machine connects
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, owner_user_id, path)
);

-- Person-level access to a directory (DECISIONS: Directories). The owner always has read_write.
CREATE TABLE directory_access (
  id           uuid PRIMARY KEY,
  directory_id uuid NOT NULL REFERENCES directories (id) ON DELETE CASCADE,
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  mode         text NOT NULL CHECK (mode IN ('none', 'read', 'read_write')),
  granted_by   uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (directory_id, user_id)
);

-- Agents in a workspace.
CREATE TABLE members (
  id                   uuid PRIMARY KEY,
  workspace_id         uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  name                 text NOT NULL,
  kind                 text NOT NULL CHECK (kind IN ('manager', 'staff')),
  skill                text CHECK (skill IN ('developer', 'reviewer', 'researcher', 'designer', 'writer')),
  reports_to           uuid REFERENCES members (id) ON DELETE SET NULL, -- null staff = independent
  cli                  text NOT NULL,
  model                text NOT NULL,
  instructions         text NOT NULL DEFAULT '',
  avatar_color         text NOT NULL,
  all_directories      boolean NOT NULL DEFAULT false, -- managers: every workspace directory
  sleeping             boolean NOT NULL DEFAULT false,
  position             integer NOT NULL DEFAULT 0,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CHECK (kind = 'staff' OR reports_to IS NULL)
);
CREATE INDEX members_workspace ON members (workspace_id);

CREATE TABLE directory_grants (
  id           uuid PRIMARY KEY,
  member_id    uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
  directory_id uuid NOT NULL REFERENCES directories (id) ON DELETE CASCADE,
  mode         text NOT NULL CHECK (mode IN ('read', 'read_write')),
  source       text NOT NULL CHECK (source IN ('suggested', 'human')),
  reason       text NOT NULL DEFAULT '',
  granted_by   uuid REFERENCES users (id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (member_id, directory_id)
);

-- How each CLI signs in for an organization. Keys never reach the hub.
CREATE TABLE cli_connections (
  id                 uuid PRIMARY KEY,
  org_id             uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  cli                text NOT NULL,
  mode               text NOT NULL CHECK (mode IN ('subscription', 'api_key')),
  cap_tokens_per_day bigint CHECK (cap_tokens_per_day > 0),
  key_fingerprint    text NOT NULL DEFAULT '',
  enabled            boolean NOT NULL DEFAULT true,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, cli)
);

-- Live usage per sign-in.
CREATE TABLE accounts (
  id            uuid PRIMARY KEY,
  org_id        uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  cli           text NOT NULL,
  mode          text NOT NULL,
  used_tokens   bigint NOT NULL DEFAULT 0,  -- through Canopy, today (API key mode)
  used_day      date NOT NULL DEFAULT current_date,
  limited_until timestamptz,               -- subscription limit reported by the CLI
  utilization   jsonb NOT NULL DEFAULT '{}', -- subscription windows reported by the CLI, when it does
  paused        boolean NOT NULL DEFAULT false,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, cli)
);

ALTER TABLE invites ADD COLUMN declined_notified boolean NOT NULL DEFAULT false;

CREATE TRIGGER runners_updated_at BEFORE UPDATE ON runners FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER runner_pairings_updated_at BEFORE UPDATE ON runner_pairings FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER workspaces_updated_at BEFORE UPDATE ON workspaces FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER workspace_access_updated_at BEFORE UPDATE ON workspace_access FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER directories_updated_at BEFORE UPDATE ON directories FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER directory_access_updated_at BEFORE UPDATE ON directory_access FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER members_updated_at BEFORE UPDATE ON members FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER directory_grants_updated_at BEFORE UPDATE ON directory_grants FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER cli_connections_updated_at BEFORE UPDATE ON cli_connections FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER accounts_updated_at BEFORE UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
ALTER TABLE invites DROP COLUMN declined_notified;
DROP TABLE accounts, cli_connections, directory_grants, members, directory_access, directories,
  workspace_access, workspaces, runner_pairings, runners;
