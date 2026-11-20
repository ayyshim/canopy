-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TABLE users (
  id            uuid PRIMARY KEY,
  email         text NOT NULL UNIQUE,          -- stored lowercased
  name          text NOT NULL,
  password_hash text,                          -- null for OAuth-only accounts
  avatar_color  text NOT NULL,
  last_org_id   uuid,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Registering creates the organization first, then its owner, in one transaction;
-- the owner foreign key is checked at commit.
CREATE TABLE organizations (
  id                    uuid PRIMARY KEY,
  name                  text NOT NULL,
  slug                  text NOT NULL UNIQUE,
  size                  text NOT NULL CHECK (size IN ('just_me', '2_10', '11_50', '51_plus')),
  about                 text NOT NULL DEFAULT '',
  owner_user_id         uuid NOT NULL REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
  default_session_limit bigint NOT NULL CHECK (default_session_limit > 0),
  default_weekly_limit  bigint NOT NULL CHECK (default_weekly_limit > 0),
  timezone              text NOT NULL DEFAULT 'UTC',
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE users
  ADD CONSTRAINT users_last_org_fk FOREIGN KEY (last_org_id) REFERENCES organizations (id) ON DELETE SET NULL;

CREATE TABLE memberships (
  id                     uuid PRIMARY KEY,
  org_id                 uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id                uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  org_role               text NOT NULL CHECK (org_role IN ('owner', 'admin', 'member')),
  status                 text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'invited')),
  personal_session_limit bigint CHECK (personal_session_limit > 0),
  personal_weekly_limit  bigint CHECK (personal_weekly_limit > 0),
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, user_id)
);
CREATE UNIQUE INDEX memberships_one_owner ON memberships (org_id) WHERE org_role = 'owner';
CREATE INDEX memberships_user ON memberships (user_id);

CREATE TABLE invites (
  id               uuid PRIMARY KEY,
  org_id           uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  email            text NOT NULL,
  org_role         text NOT NULL CHECK (org_role IN ('admin', 'member')),
  token_hash       bytea NOT NULL UNIQUE,
  invited_by       uuid REFERENCES users (id) ON DELETE SET NULL,
  expires_at       timestamptz NOT NULL,
  accepted_at      timestamptz,
  declined_at      timestamptz,
  revoked_at       timestamptz,
  workspace_grants jsonb NOT NULL DEFAULT '[]',
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invites_org ON invites (org_id);

CREATE TABLE user_identities (
  id         uuid PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  provider   text NOT NULL CHECK (provider IN ('github', 'google')),
  subject    text NOT NULL,
  email      text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, subject)
);

CREATE TABLE auth_sessions (
  id           uuid PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash   bytea NOT NULL UNIQUE,
  expires_at   timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  user_agent   text NOT NULL DEFAULT '',
  ip           text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_sessions_user ON auth_sessions (user_id);

CREATE TABLE password_resets (
  id         uuid PRIMARY KEY,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_events (
  id            uuid PRIMARY KEY,
  org_id        uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  workspace_id  uuid,
  actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  type          text NOT NULL,
  summary       text NOT NULL,
  at            timestamptz NOT NULL DEFAULT now(),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_org_at ON audit_events (org_id, at DESC);

CREATE TRIGGER users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER organizations_updated_at BEFORE UPDATE ON organizations FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER memberships_updated_at BEFORE UPDATE ON memberships FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER invites_updated_at BEFORE UPDATE ON invites FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER user_identities_updated_at BEFORE UPDATE ON user_identities FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER auth_sessions_updated_at BEFORE UPDATE ON auth_sessions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER password_resets_updated_at BEFORE UPDATE ON password_resets FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER audit_events_updated_at BEFORE UPDATE ON audit_events FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE audit_events, password_resets, auth_sessions, user_identities, invites, memberships;
ALTER TABLE users DROP CONSTRAINT users_last_org_fk;
DROP TABLE organizations, users;
DROP FUNCTION set_updated_at();
