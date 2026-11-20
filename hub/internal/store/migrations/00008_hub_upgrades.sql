-- +goose Up
-- Every time a new hub binary upgraded the data: shown on the hub admin dashboard.
CREATE TABLE hub_upgrades (
  id           uuid PRIMARY KEY,
  from_version text NOT NULL,
  to_version   text NOT NULL,
  from_schema  bigint NOT NULL,
  to_schema    bigint NOT NULL,
  changes      jsonb NOT NULL DEFAULT '[]',
  backup_path  text NOT NULL DEFAULT '',
  at           timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE hub_upgrades;
