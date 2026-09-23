-- M2 schema v1: jobs, destinations, executor state.
-- Shares the DB file (and schema_version line) with the library package,
-- which owns library_items. All statements are IF NOT EXISTS / OR IGNORE
-- so either package can migrate first.
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS jobs (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  library_item_id  INTEGER NOT NULL REFERENCES library_items(id),
  destination_id   INTEGER NOT NULL REFERENCES destinations(id),
  kind             TEXT NOT NULL,
  "order"          INTEGER NOT NULL,
  status           TEXT NOT NULL DEFAULT 'pending',
  phase            TEXT NOT NULL DEFAULT '',
  bytes_total      INTEGER NOT NULL DEFAULT 0,
  bytes_done       INTEGER NOT NULL DEFAULT 0,
  error            TEXT NOT NULL DEFAULT '',
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS destinations (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  path        TEXT NOT NULL,
  kind        TEXT NOT NULL DEFAULT 'folder',
  filesystem  TEXT NOT NULL DEFAULT 'unknown',
  fs_override TEXT NOT NULL DEFAULT '',
  bdm_prefix  TEXT NOT NULL DEFAULT '',
  free_bytes  INTEGER NOT NULL DEFAULT 0,
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Single-row-per-key executor state: paused flag, heartbeats.
CREATE TABLE IF NOT EXISTS queue_state (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Cross-process destination locks: one row per held destination.
CREATE TABLE IF NOT EXISTS dest_locks (
  destination_id INTEGER PRIMARY KEY,
  owner          TEXT NOT NULL,
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX IF NOT EXISTS idx_jobs_status_order
  ON jobs(status, "order");
CREATE INDEX IF NOT EXISTS idx_jobs_dest_status
  ON jobs(destination_id, status);
