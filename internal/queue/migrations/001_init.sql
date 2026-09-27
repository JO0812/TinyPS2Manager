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
  destination_path TEXT NOT NULL DEFAULT '',
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

-- NOTE: the destinations registry and id-keyed dest_locks lived here until
-- the live-path migration (schema v5) removed them. They must NOT be
-- recreated: re-running this file on a migrated database would resurrect
-- legacy objects (and the old jobs index below), breaking subsequent
-- Opens. Legacy databases carry those objects until
-- migrateLiveDestinations drops them explicitly.

-- Single-row-per-key executor state: paused flag, heartbeats.
CREATE TABLE IF NOT EXISTS queue_state (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Cross-process destination locks: one row per held destination.
-- (Created path-keyed by migrateLiveDestinations; see note above.)

CREATE INDEX IF NOT EXISTS idx_jobs_status_order
  ON jobs(status, "order");
-- NOTE: the (destination_id, status) index lived here until the live-path
-- migration (schema v5) dropped the column. It must NOT be recreated here:
-- re-running this file on a migrated database would fail with "no such
-- column: destination_id". Legacy databases carry the index until
-- migrateLiveDestinations drops it explicitly.
