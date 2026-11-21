-- +goose Up
ALTER TABLE goals ADD COLUMN number integer;
ALTER TABLE tasks ADD COLUMN number integer;
ALTER TABLE notes ADD COLUMN number integer;

UPDATE goals g SET number = r.n
FROM (SELECT id, row_number() OVER (PARTITION BY workspace_id ORDER BY created_at, id) AS n FROM goals) r
WHERE g.id = r.id;
UPDATE tasks t SET number = r.n
FROM (SELECT id, row_number() OVER (PARTITION BY workspace_id ORDER BY created_at, id) AS n FROM tasks) r
WHERE t.id = r.id;
UPDATE notes t SET number = r.n
FROM (SELECT id, row_number() OVER (PARTITION BY workspace_id ORDER BY created_at, id) AS n FROM notes) r
WHERE t.id = r.id;

ALTER TABLE goals ALTER COLUMN number SET NOT NULL;
ALTER TABLE tasks ALTER COLUMN number SET NOT NULL;
ALTER TABLE notes ALTER COLUMN number SET NOT NULL;
ALTER TABLE goals ADD CONSTRAINT goals_ws_number UNIQUE (workspace_id, number);
ALTER TABLE tasks ADD CONSTRAINT tasks_ws_number UNIQUE (workspace_id, number);
ALTER TABLE notes ADD CONSTRAINT notes_ws_number UNIQUE (workspace_id, number);

-- Last number handed out per workspace and type.
CREATE TABLE item_counters (
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  kind         text NOT NULL CHECK (kind IN ('goal', 'task', 'note')),
  last         integer NOT NULL,
  PRIMARY KEY (workspace_id, kind)
);
INSERT INTO item_counters (workspace_id, kind, last)
SELECT workspace_id, 'goal', max(number) FROM goals GROUP BY workspace_id;
INSERT INTO item_counters (workspace_id, kind, last)
SELECT workspace_id, 'task', max(number) FROM tasks GROUP BY workspace_id;
INSERT INTO item_counters (workspace_id, kind, last)
SELECT workspace_id, 'note', max(number) FROM notes GROUP BY workspace_id;

-- The counter row stays locked until the inserting transaction ends, so concurrent
-- inserts in one workspace queue up and never take the same number.
-- +goose StatementBegin
CREATE FUNCTION next_item_number(ws uuid, k text) RETURNS integer
LANGUAGE sql AS $$
  INSERT INTO item_counters (workspace_id, kind, last) VALUES (ws, k, 1)
  ON CONFLICT (workspace_id, kind) DO UPDATE SET last = item_counters.last + 1
  RETURNING last
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION next_item_number(uuid, text);
DROP TABLE item_counters;
ALTER TABLE notes DROP COLUMN number;
ALTER TABLE tasks DROP COLUMN number;
ALTER TABLE goals DROP COLUMN number;
