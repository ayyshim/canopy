-- +goose Up
ALTER TABLE notification_outbox DROP CONSTRAINT notification_outbox_kind_check;
ALTER TABLE notification_outbox ADD CONSTRAINT notification_outbox_kind_check
  CHECK (kind IN ('questions', 'progress', 'goal_ready', 'errors', 'jobs'));

-- +goose Down
DELETE FROM notification_outbox WHERE kind = 'jobs';
ALTER TABLE notification_outbox DROP CONSTRAINT notification_outbox_kind_check;
ALTER TABLE notification_outbox ADD CONSTRAINT notification_outbox_kind_check
  CHECK (kind IN ('questions', 'progress', 'goal_ready', 'errors'));
