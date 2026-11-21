-- +goose Up
ALTER TABLE goals ADD COLUMN discord_thread_id text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE goals DROP COLUMN discord_thread_id;
