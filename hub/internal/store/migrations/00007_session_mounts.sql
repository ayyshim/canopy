-- +goose Up
ALTER TABLE sessions ADD COLUMN mounts jsonb NOT NULL DEFAULT '[]';
ALTER TABLE sessions ADD COLUMN cwd text NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN use_key_until timestamptz;    -- "Use an API key until reset"
ALTER TABLE accounts ADD COLUMN cap_warned_day date;           -- 80% warning shown today
ALTER TABLE tasks ADD COLUMN nudges integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE tasks DROP COLUMN nudges;
ALTER TABLE accounts DROP COLUMN cap_warned_day;
ALTER TABLE accounts DROP COLUMN use_key_until;
ALTER TABLE sessions DROP COLUMN cwd;
ALTER TABLE sessions DROP COLUMN mounts;
