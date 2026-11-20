-- +goose Up
ALTER TABLE goals ADD COLUMN description text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE goals DROP COLUMN description;
