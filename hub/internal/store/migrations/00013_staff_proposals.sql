-- +goose Up
-- Drafts a manager writes for a new staff member, or for new instructions on an existing one.
-- They wait here until a workspace admin approves or rejects them.
CREATE TABLE staff_proposals (
  id           uuid PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  manager_id   uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
  kind         text NOT NULL CHECK (kind IN ('new', 'update')),
  member_id    uuid REFERENCES members (id) ON DELETE CASCADE, -- the staff member an update changes
  name         text NOT NULL DEFAULT '',
  role         text CHECK (role IN ('developer', 'reviewer', 'researcher', 'designer', 'writer')),
  instructions text NOT NULL DEFAULT '',
  skills       jsonb NOT NULL DEFAULT '[]', -- [{name, content, source_url}]
  status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
  decided_by   uuid REFERENCES users (id) ON DELETE SET NULL,
  decided_at   timestamptz,
  reason       text NOT NULL DEFAULT '',
  result_member_id uuid REFERENCES members (id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CHECK (kind = 'new' OR member_id IS NOT NULL),
  CHECK (kind = 'update' OR role IS NOT NULL)
);
CREATE INDEX staff_proposals_ws ON staff_proposals (workspace_id, status, created_at);
CREATE TRIGGER staff_proposals_updated_at BEFORE UPDATE ON staff_proposals FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE staff_proposals;
