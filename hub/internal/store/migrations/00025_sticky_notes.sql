-- +goose Up
ALTER TABLE notes
  ADD COLUMN visibility text NOT NULL DEFAULT 'shared' CHECK (visibility IN ('shared', 'private')),
  ADD COLUMN color text NOT NULL DEFAULT 'yellow' CHECK (color IN ('yellow', 'green', 'blue', 'pink', 'purple', 'orange', 'gray')),
  ADD COLUMN board_x double precision,
  ADD COLUMN board_y double precision,
  ADD COLUMN edited_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  ADD COLUMN edited_by_member_id uuid REFERENCES members (id) ON DELETE SET NULL,
  ADD CONSTRAINT notes_board_pos CHECK ((board_x IS NULL) = (board_y IS NULL)),
  ADD CONSTRAINT notes_private_author CHECK (visibility = 'shared' OR author_user_id IS NOT NULL);

-- The last 20 versions of a note, saved before each change.
CREATE TABLE note_revisions (
  id                uuid PRIMARY KEY,
  note_id           uuid NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
  rev               integer NOT NULL,
  title             text NOT NULL,
  body              text NOT NULL,
  kind              text NOT NULL,
  edited_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
  edited_by_member_id uuid REFERENCES members (id) ON DELETE SET NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (note_id, rev)
);

-- +goose Down
DROP TABLE note_revisions;
ALTER TABLE notes
  DROP CONSTRAINT notes_private_author, DROP CONSTRAINT notes_board_pos,
  DROP COLUMN edited_by_member_id, DROP COLUMN edited_by_user_id,
  DROP COLUMN board_y, DROP COLUMN board_x, DROP COLUMN color, DROP COLUMN visibility;
