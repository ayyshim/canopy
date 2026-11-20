-- +goose Up
-- Every state change is an event (SPEC "Reliability"); boards get a snapshot, then these.
CREATE TABLE events (
  seq          bigserial PRIMARY KEY,
  org_id       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  workspace_id uuid REFERENCES workspaces (id) ON DELETE CASCADE, -- null: organization-wide
  type         text NOT NULL,
  member_id    uuid,          -- the agent the event belongs to, for staff visibility
  admin_only   boolean NOT NULL DEFAULT false,
  data         jsonb NOT NULL,
  at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_workspace ON events (workspace_id, seq);
CREATE INDEX events_org ON events (org_id, seq) WHERE workspace_id IS NULL;

-- +goose Down
DROP TABLE events;
