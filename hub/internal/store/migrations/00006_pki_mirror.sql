-- +goose Up
-- The hub's certificate authority for runner mutual TLS.
CREATE TABLE hub_pki (
  id          boolean PRIMARY KEY DEFAULT true CHECK (id),
  ca_cert_pem text NOT NULL,
  ca_key_pem  text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE runners ADD COLUMN address text NOT NULL DEFAULT '';

-- Hub copy of each shared directory (DECISIONS: Directories).
CREATE TABLE mirror_state (
  directory_id   uuid PRIMARY KEY REFERENCES directories (id) ON DELETE CASCADE,
  revision       bigint NOT NULL DEFAULT 0,
  complete       boolean NOT NULL DEFAULT false, -- a full tree has arrived at least once
  missing        boolean NOT NULL DEFAULT false, -- the owner machine reports no such directory
  last_synced_at timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE mirror_files (
  directory_id uuid NOT NULL REFERENCES directories (id) ON DELETE CASCADE,
  path         text NOT NULL,
  sha256       text NOT NULL,
  size         bigint NOT NULL,
  mode         integer NOT NULL DEFAULT 420,
  revision     bigint NOT NULL,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (directory_id, path)
);

-- Changes made away from the owner's machine, waiting to land on its disk.
CREATE TABLE pending_writes (
  id           uuid PRIMARY KEY,
  directory_id uuid NOT NULL REFERENCES directories (id) ON DELETE CASCADE,
  path         text NOT NULL,
  base_sha     text NOT NULL DEFAULT '',
  new_sha      text NOT NULL DEFAULT '', -- '' = delete
  mode         integer NOT NULL DEFAULT 420,
  member_id    uuid REFERENCES members (id) ON DELETE SET NULL,
  task_id      uuid REFERENCES tasks (id) ON DELETE SET NULL,
  status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'applied', 'conflict', 'discarded')),
  current_sha  text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  applied_at   timestamptz
);
CREATE INDEX pending_writes_queued ON pending_writes (directory_id, created_at) WHERE status = 'queued';

CREATE TRIGGER mirror_state_updated_at BEFORE UPDATE ON mirror_state FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER pending_writes_updated_at BEFORE UPDATE ON pending_writes FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE pending_writes, mirror_files, mirror_state;
ALTER TABLE runners DROP COLUMN address;
DROP TABLE hub_pki;
