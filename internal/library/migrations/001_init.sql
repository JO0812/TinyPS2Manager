-- M1 schema v1: library items (spec §8). Queue tables land in M2.
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS library_items (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  source_path      TEXT NOT NULL,
  content_hash     TEXT NOT NULL,
  platform         TEXT NOT NULL,
  disc_type        TEXT NOT NULL DEFAULT '',
  detection_method TEXT NOT NULL DEFAULT '',
  title            TEXT NOT NULL DEFAULT '',
  disc_index       INTEGER NOT NULL DEFAULT 0,
  disc_group_id    INTEGER NULL,
  size_bytes       INTEGER NOT NULL DEFAULT 0,
  status           TEXT NOT NULL DEFAULT 'new',
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_library_items_hash
  ON library_items(content_hash);
CREATE INDEX IF NOT EXISTS idx_library_items_group
  ON library_items(disc_group_id);
