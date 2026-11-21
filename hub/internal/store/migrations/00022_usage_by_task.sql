-- +goose Up
ALTER TABLE usage_events
  ADD COLUMN task_id     uuid REFERENCES tasks (id) ON DELETE SET NULL,
  ADD COLUMN goal_id     uuid REFERENCES goals (id) ON DELETE SET NULL,
  ADD COLUMN cache_read  bigint NOT NULL DEFAULT 0,
  ADD COLUMN cache_write bigint NOT NULL DEFAULT 0,
  ADD COLUMN cost_micros bigint NOT NULL DEFAULT 0;
CREATE INDEX usage_events_task ON usage_events (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX usage_events_goal ON usage_events (goal_id) WHERE goal_id IS NOT NULL;

ALTER TABLE usage_hourly
  ADD COLUMN cache_read  bigint NOT NULL DEFAULT 0,
  ADD COLUMN cache_write bigint NOT NULL DEFAULT 0,
  ADD COLUMN cost_micros bigint NOT NULL DEFAULT 0;

-- Running totals per goal, task and model. The zero uuid means "none" (work outside a task).
CREATE TABLE usage_work (
  goal_id      uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  task_id      uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
  model        text NOT NULL,
  tokens_in    bigint NOT NULL DEFAULT 0,
  tokens_out   bigint NOT NULL DEFAULT 0,
  cache_read   bigint NOT NULL DEFAULT 0,
  cache_write  bigint NOT NULL DEFAULT 0,
  calls        bigint NOT NULL DEFAULT 0,
  cost_micros  bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (goal_id, task_id, model)
);

-- List prices in dollars per million tokens. A row applies when its pattern is part of the model
-- name; the longest pattern wins. Edit rows to change prices; old events keep the cost they had.
CREATE TABLE model_prices (
  pattern      text PRIMARY KEY,
  input        double precision NOT NULL,
  output       double precision NOT NULL,
  cache_read   double precision NOT NULL,
  cache_write  double precision NOT NULL
);
INSERT INTO model_prices (pattern, input, output, cache_read, cache_write) VALUES
  ('fable-5-1', 10, 50, 0.25, 12.5),
  ('mythos-5-1', 10, 50, 0.25, 12.5),
  ('fable', 10, 50, 1, 12.5),
  ('mythos', 10, 50, 1, 12.5),
  ('opus-5-5', 4, 20, 0.2, 5),
  ('opus-5', 5, 25, 0.5, 6.25),
  ('opus-4', 5, 25, 0.5, 6.25),
  ('opus', 4, 20, 0.2, 5),
  ('sonnet-5', 2, 10, 0.2, 2.5),
  ('sonnet-4', 3, 15, 0.3, 3.75),
  ('sonnet', 2, 10, 0.2, 2.5),
  ('haiku-5', 0.1, 0.5, 0.01, 0.125),
  ('haiku-4', 1, 5, 0.1, 1.25),
  ('haiku', 0.1, 0.5, 0.01, 0.125);

-- +goose Down
DROP TABLE model_prices;
DROP TABLE usage_work;
ALTER TABLE usage_hourly DROP COLUMN cache_read, DROP COLUMN cache_write, DROP COLUMN cost_micros;
ALTER TABLE usage_events DROP COLUMN task_id, DROP COLUMN goal_id, DROP COLUMN cache_read, DROP COLUMN cache_write, DROP COLUMN cost_micros;
