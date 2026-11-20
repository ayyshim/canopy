-- +goose Up
-- A per-workspace skill library: markdown people upload, or a GitHub link fetched and stored.
-- Members get library skills attached; the content goes into their prompt.
CREATE TABLE workspace_skills (
  id           uuid PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  name         text NOT NULL,
  source_kind  text NOT NULL CHECK (source_kind IN ('md', 'github')),
  source_url   text NOT NULL DEFAULT '',
  content      text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, name)
);

CREATE TABLE member_skills (
  member_id  uuid NOT NULL REFERENCES members (id) ON DELETE CASCADE,
  skill_id   uuid NOT NULL REFERENCES workspace_skills (id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (member_id, skill_id)
);
CREATE INDEX member_skills_skill ON member_skills (skill_id);

-- +goose Down
DROP TABLE member_skills;
DROP TABLE workspace_skills;
