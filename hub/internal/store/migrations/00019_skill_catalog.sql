-- +goose Up
-- Skills are now catalog keys like 'marketing.seo'. The hub validates them in Go.
ALTER TABLE members DROP CONSTRAINT IF EXISTS members_skill_check;
ALTER TABLE staff_proposals DROP CONSTRAINT IF EXISTS staff_proposals_role_check;

-- +goose Down
ALTER TABLE members ADD CONSTRAINT members_skill_check
    CHECK (skill IN ('developer', 'reviewer', 'researcher', 'designer', 'writer')) NOT VALID;
ALTER TABLE staff_proposals ADD CONSTRAINT staff_proposals_role_check
    CHECK (role IN ('developer', 'reviewer', 'researcher', 'designer', 'writer')) NOT VALID;
