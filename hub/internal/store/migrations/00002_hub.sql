-- +goose Up
-- A hub is one self-hosted Canopy server. It is set up once (name, sign-in providers, first hub
-- admin) before any organization can register.
CREATE TABLE hub_settings (
  id                 boolean PRIMARY KEY DEFAULT true CHECK (id), -- exactly one row
  name               text NOT NULL,
  signup_open        boolean NOT NULL DEFAULT true,               -- anyone may register an organization
  setup_completed_at timestamptz NOT NULL DEFAULT now(),
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE auth_providers (
  provider      text PRIMARY KEY CHECK (provider IN ('github', 'google')),
  enabled       boolean NOT NULL DEFAULT false,
  client_id     text NOT NULL DEFAULT '',
  client_secret text NOT NULL DEFAULT '', -- write-only through the API
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE users ADD COLUMN is_hub_admin boolean NOT NULL DEFAULT false;

-- Hub-level events (setup, settings, hub admin changes) have no organization.
ALTER TABLE audit_events ALTER COLUMN org_id DROP NOT NULL;
CREATE INDEX audit_events_hub_at ON audit_events (at DESC) WHERE org_id IS NULL;

CREATE TRIGGER hub_settings_updated_at BEFORE UPDATE ON hub_settings FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER auth_providers_updated_at BEFORE UPDATE ON auth_providers FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP INDEX audit_events_hub_at;
DELETE FROM audit_events WHERE org_id IS NULL;
ALTER TABLE audit_events ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE users DROP COLUMN is_hub_admin;
DROP TABLE auth_providers, hub_settings;
