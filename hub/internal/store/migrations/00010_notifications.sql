-- +goose Up
-- Notification choices per person ({"questions": {"desktop": true, "email": true}, ...}),
-- the day the daily summary last went out, and which signals were emailed.
ALTER TABLE users ADD COLUMN notifications jsonb NOT NULL DEFAULT '{}';
ALTER TABLE users ADD COLUMN daily_sent_on date;
ALTER TABLE signals ADD COLUMN emailed_at timestamptz;
-- Signals already open when this ships aren't emailed.
UPDATE signals SET emailed_at = now() WHERE status = 'open';
CREATE INDEX signals_unmailed ON signals (created_at) WHERE status = 'open' AND emailed_at IS NULL;

-- +goose Down
DROP INDEX signals_unmailed;
ALTER TABLE signals DROP COLUMN emailed_at;
ALTER TABLE users DROP COLUMN daily_sent_on;
ALTER TABLE users DROP COLUMN notifications;
