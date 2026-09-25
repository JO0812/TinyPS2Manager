package queue

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/001_init.sql
var migration001 string

// migrateAttempts adds jobs.attempt_count (schema v4), PRAGMA-guarded
// like migrateCapacity.
func migrateAttempts(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(jobs)`)
	if err != nil {
		return fmt.Errorf("inspect jobs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "attempt_count" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add attempt_count: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO schema_version(version) VALUES (4)`); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

// lockStaleAfter bounds cross-process lock takeovers: a heartbeat older
// than this means the holder died without releasing.
const lockStaleAfter = 60 * time.Second

// Store is the SQLite-backed job/destination/executor-state store. It shares
// the DB file with the library package (additive migrations, idempotent).
// Single-writer discipline: exactly one executor writes per destination, and
// the pool holds a single connection to avoid SQLITE_BUSY churn.
type Store struct {
	db *sql.DB
}

// Open runs pending migrations. path may be ":memory:" for tests.
// Shared-file discipline with the library package: WAL mode (readers
// never block the single writer), a 5s busy timeout so cross-pool writers
// serialize instead of failing, and one connection.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(migration001); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate queue schema: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO schema_version(version) VALUES (2)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("record schema version: %w", err)
	}
	if err := migrateCapacity(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateAttempts(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrateCapacity adds destinations.total_bytes (schema v3). SQLite has no
// idempotent ADD COLUMN, so the PRAGMA guard keeps Open re-runnable.
func migrateCapacity(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(destinations)`)
	if err != nil {
		return fmt.Errorf("inspect destinations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == "total_bytes" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE destinations ADD COLUMN total_bytes INTEGER NOT NULL DEFAULT -1`); err != nil {
		return fmt.Errorf("add total_bytes: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO schema_version(version) VALUES (3)`); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// --- Destinations ---

// AddDestination inserts a target and returns it with its ID.
func (s *Store) AddDestination(d Destination) (Destination, error) {
	if d.Path == "" {
		return Destination{}, fmt.Errorf("destination path is empty")
	}
	if d.Kind != DestDrive && d.Kind != DestFolder {
		return Destination{}, fmt.Errorf("bad destination kind %q", d.Kind)
	}
	res, err := s.db.Exec(`INSERT INTO destinations
		(path, kind, filesystem, fs_override, bdm_prefix, free_bytes, total_bytes)
		VALUES (?,?,?,?,?,?,?)`,
		d.Path, string(d.Kind), d.Filesystem, d.FSOverride, d.BDMPrefix, d.FreeBytes, d.TotalBytes)
	if err != nil {
		return Destination{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Destination{}, err
	}
	d.ID = id
	return d, nil
}

// GetDestination returns one target, or nil.
func (s *Store) GetDestination(id int64) (*Destination, error) {
	row := s.db.QueryRow(`SELECT id, path, kind, filesystem, fs_override,
		bdm_prefix, free_bytes, total_bytes, updated_at FROM destinations WHERE id=?`, id)
	return scanDestination(row)
}

// ListDestinations returns all targets in insertion order.
func (s *Store) ListDestinations() ([]Destination, error) {
	rows, err := s.db.Query(`SELECT id, path, kind, filesystem, fs_override,
		bdm_prefix, free_bytes, total_bytes, updated_at FROM destinations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Destination
	for rows.Next() {
		d, err := scanDestination(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// UpdateDestinationPrefix sets the BDM prefix.
func (s *Store) UpdateDestinationPrefix(id int64, prefix string) error {
	_, err := s.db.Exec(`UPDATE destinations SET bdm_prefix=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, prefix, id)
	return err
}

// UpdateDestinationOverride sets the explicit filesystem choice
// ("" clears it back to detection).
func (s *Store) UpdateDestinationOverride(id int64, override string) error {
	if override != "" && override != "fat32" && override != "exfat" {
		return fmt.Errorf("bad filesystem override %q", override)
	}
	res, err := s.db.Exec(`UPDATE destinations SET fs_override=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, override, id)
	if err != nil {
		return err
	}
	return expectOne(res, id, "override (missing destination)")
}

// ErrDestinationHasJobs is returned by DeleteDestination when jobs still
// reference the destination.
var ErrDestinationHasJobs = fmt.Errorf("destination has jobs")

// DeleteDestination removes a tracked destination. It refuses when jobs
// still reference it (remove or finish them first) and drops any lock row
// alongside, so a re-added drive starts clean.
func (s *Store) DeleteDestination(id int64) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE destination_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: destination %d has %d job(s)", ErrDestinationHasJobs, id, n)
	}
	res, err := s.db.Exec(`DELETE FROM destinations WHERE id=?`, id)
	if err != nil {
		return err
	}
	if err := expectOne(res, id, "destination (missing destination)"); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM dest_locks WHERE destination_id=?`, id)
	return err
}

// RefreshDestinationStats records freshly probed filesystem/free space.
func (s *Store) RefreshDestinationStats(id int64, filesystem string, freeBytes, totalBytes int64) error {
	_, err := s.db.Exec(`UPDATE destinations SET filesystem=?, free_bytes=?, total_bytes=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`,
		filesystem, freeBytes, totalBytes, id)
	return err
}

func scanDestination(r rowScanner) (*Destination, error) {
	var d Destination
	var kind string
	err := r.Scan(&d.ID, &d.Path, &kind, &d.Filesystem, &d.FSOverride,
		&d.BDMPrefix, &d.FreeBytes, &d.TotalBytes, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Kind = DestinationKind(kind)
	return &d, nil
}

// --- Jobs ---

// Enqueue appends jobs in order, assigning Order after the current maximum.
func (s *Store) Enqueue(jobs []Job) ([]Job, error) {
	var max sql.NullInt64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX("order"),-1) FROM jobs`).Scan(&max); err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(jobs))
	for _, j := range jobs {
		if !validKind(j.Kind) {
			return nil, fmt.Errorf("bad job kind %q", j.Kind)
		}
		if j.Status == "" {
			j.Status = JobPending
		}
		max.Int64++
		res, err := s.db.Exec(`INSERT INTO jobs
			(library_item_id, destination_id, kind, "order", status, phase,
			 bytes_total, bytes_done, error)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			j.LibraryItemID, j.DestinationID, string(j.Kind), max.Int64,
			string(j.Status), j.Phase, j.BytesTotal, j.BytesDone, j.Error)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		j.ID, j.Order = id, int(max.Int64)
		out = append(out, j)
	}
	return out, nil
}

func validKind(k JobKind) bool {
	switch k {
	case KindCopy, KindSplitAndCopy, KindConvertCopy, KindEmberCopy, KindEnrich:
		return true
	}
	return false
}

// GetJob returns one job, or nil.
func (s *Store) GetJob(id int64) (*Job, error) {
	row := s.db.QueryRow(`SELECT id, library_item_id, destination_id, kind,
		"order", status, phase, bytes_total, bytes_done, error, attempt_count,
		created_at, updated_at FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

// ListJobs returns all jobs in queue order.
func (s *Store) ListJobs() ([]Job, error) {
	rows, err := s.db.Query(`SELECT id, library_item_id, destination_id, kind,
		"order", status, phase, bytes_total, bytes_done, error, attempt_count,
		created_at, updated_at FROM jobs ORDER BY "order"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// PeekPending returns the lowest-order pending job for a destination
// WITHOUT claiming it, so the executor can prepare it in the background
// while the current job writes. The claim still goes through NextPending,
// which skips per-job paused entries and races safely.
func (s *Store) PeekPending(destinationID int64) (job *Job, ok bool, err error) {
	var id int64
	err = s.db.QueryRow(`SELECT id FROM jobs
		WHERE destination_id=? AND status=? ORDER BY "order" LIMIT 1`,
		destinationID, string(JobPending)).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	job, err = s.GetJob(id)
	if err != nil || job == nil {
		return nil, false, err
	}
	return job, true, nil
}

// NextPending claims the lowest-order pending job for a destination,
// skipping per-job paused entries. ok=false when the queue is dry. The
// UPDATE is atomic: two executors racing claim different rows.
func (s *Store) NextPending(destinationID int64) (job *Job, ok bool, err error) {
	var id int64
	err = s.db.QueryRow(`SELECT id FROM jobs
		WHERE destination_id=? AND status=? ORDER BY "order" LIMIT 1`,
		destinationID, string(JobPending)).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	res, err := s.db.Exec(`UPDATE jobs SET status=?, phase=?, error='',
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND status=?`,
		string(JobRunning), "", id, string(JobPending))
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil // lost the race; caller retries
	}
	job, err = s.GetJob(id)
	if err != nil || job == nil {
		return nil, false, err
	}
	return job, true, nil
}

// Checkpoint records progress (throttled by the caller to ~1%/1s).
func (s *Store) Checkpoint(id int64, phase string, bytesDone int64) error {
	_, err := s.db.Exec(`UPDATE jobs SET phase=?, bytes_done=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`,
		phase, bytesDone, id)
	return err
}

// FinishJob marks completion (error=="" → done, else error state + message).
func (s *Store) FinishJob(id int64, jobErr string) error {
	status := string(JobDone)
	if jobErr != "" {
		status = string(JobError)
	}
	_, err := s.db.Exec(`UPDATE jobs SET status=?, phase=?, error=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`,
		status, "", jobErr, id)
	return err
}

// RequeueJob returns a job to pending, clearing error and progress
// (post-cancel restart; crash-recovered running jobs).
func (s *Store) RequeueJob(id int64) error {
	res, err := s.db.Exec(`UPDATE jobs SET status=?, phase='', error='',
		bytes_done=0, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND status!=?`, string(JobPending), id, string(JobDone))
	if err != nil {
		return err
	}
	return expectOne(res, id, "requeue (done or missing)")
}

// RetryJob requeues an errored job, clearing error, progress, and the
// attempt counter (manual retry always starts fresh).
func (s *Store) RetryJob(id int64) error {
	res, err := s.db.Exec(`UPDATE jobs SET status=?, error='', bytes_done=0,
		attempt_count=0,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND status=?`, string(JobPending), id, string(JobError))
	if err != nil {
		return err
	}
	return expectOne(res, id, "retry (not in error)")
}

// IncrementAttempts records one failed try, returning the new count.
func (s *Store) IncrementAttempts(id int64) (int, error) {
	if _, err := s.db.Exec(`UPDATE jobs SET attempt_count=attempt_count+1,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, id); err != nil {
		return 0, err
	}
	var n int
	if err := s.db.QueryRow(`SELECT attempt_count FROM jobs WHERE id=?`, id).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// PauseJob / ResumeJob flip a pending or errored job's paused state.
func (s *Store) PauseJob(id int64) error {
	res, err := s.db.Exec(`UPDATE jobs SET status=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND status IN (?,?)`,
		string(JobPaused), id, string(JobPending), string(JobError))
	if err != nil {
		return err
	}
	return expectOne(res, id, "pause (not pending/error)")
}

// ResumeJob returns a paused job to pending.
func (s *Store) ResumeJob(id int64) error {
	res, err := s.db.Exec(`UPDATE jobs SET status=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND status=?`, string(JobPending), id, string(JobPaused))
	if err != nil {
		return err
	}
	return expectOne(res, id, "resume (not paused)")
}

// CancelJob deletes a non-running job. Running jobs stop via context; the
// executor removes the row after cleaning the partial file.
func (s *Store) CancelJob(id int64) error {
	res, err := s.db.Exec(`DELETE FROM jobs WHERE id=? AND status!=?`,
		id, string(JobRunning))
	if err != nil {
		return err
	}
	return expectOne(res, id, "cancel (running or missing — stop the executor first)")
}

// DeleteJob removes a job row unconditionally (executor post-cleanup).
func (s *Store) DeleteJob(id int64) error {
	_, err := s.db.Exec(`DELETE FROM jobs WHERE id=?`, id)
	return err
}

// Reorder moves a job to a zero-based position among all jobs, renumbering.
func (s *Store) Reorder(id int64, position int) error {
	jobs, err := s.ListJobs()
	if err != nil {
		return err
	}
	var target *Job
	rest := jobs[:0:0]
	for _, j := range jobs {
		if j.ID == id {
			c := j
			target = &c
		} else {
			rest = append(rest, j)
		}
	}
	if target == nil {
		return fmt.Errorf("no job %d", id)
	}
	if position < 0 {
		position = 0
	}
	if position > len(rest) {
		position = len(rest)
	}
	ordered := append(append([]Job{}, rest[:position]...), *target)
	ordered = append(ordered, rest[position:]...)
	for i, j := range ordered {
		if _, err := s.db.Exec(`UPDATE jobs SET "order"=?,
			updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, i, j.ID); err != nil {
			return err
		}
	}
	return nil
}

// InFlightBytes sums bytes_total minus bytes_done over active jobs for a
// destination (free-space accounting at enqueue).
func (s *Store) InFlightBytes(destinationID int64) (int64, error) {
	var total sql.NullInt64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(bytes_total-bytes_done),0)
		FROM jobs WHERE destination_id=? AND status IN (?,?,?)`,
		destinationID, string(JobPending), string(JobRunning), string(JobPaused)).Scan(&total)
	return total.Int64, err
}

func expectOne(res sql.Result, id int64, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("job %d: cannot %s", id, what)
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanJob(r rowScanner) (*Job, error) {
	var j Job
	var kind, status string
	err := r.Scan(&j.ID, &j.LibraryItemID, &j.DestinationID, &kind,
		&j.Order, &status, &j.Phase, &j.BytesTotal, &j.BytesDone, &j.Error, &j.Attempts,
		&j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.Kind, j.Status = JobKind(kind), JobStatus(status)
	return &j, nil
}

// --- Global pause flag ---

const pausedKey = "paused"

// SetState upserts a queue_state key (unstructured executor metadata,
// e.g. staged loader versions per destination).
func (s *Store) SetState(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO queue_state(key, value,
		updated_at) VALUES (?,?,
		strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,
		updated_at=excluded.updated_at`, key, value)
	return err
}

// GetState reads a queue_state key.
func (s *Store) GetState(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM queue_state WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetPaused flips the whole-queue pause flag.
func (s *Store) SetPaused(paused bool) error {
	v := "0"
	if paused {
		v = "1"
	}
	_, err := s.db.Exec(`INSERT INTO queue_state(key, value,
		updated_at) VALUES (?,?,
		strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,
		updated_at=excluded.updated_at`, pausedKey, v)
	return err
}

// Paused reports the whole-queue pause flag (absent row = running).
func (s *Store) Paused() (bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM queue_state WHERE key=?`, pausedKey).Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v == "1", nil
}

// --- Cross-process destination locks ---

// AcquireLock claims the destination for owner (hostname:pid:token). A
// missing row is created; a fresh heartbeat refuses; a stale one is taken
// over (previous holder died without releasing).
func (s *Store) AcquireLock(destinationID int64, owner string) error {
	var cur string
	var updated string
	err := s.db.QueryRow(`SELECT owner, updated_at FROM dest_locks
		WHERE destination_id=?`, destinationID).Scan(&cur, &updated)
	switch {
	case err == sql.ErrNoRows:
		_, err := s.db.Exec(`INSERT INTO dest_locks(destination_id, owner) VALUES (?,?)`,
			destinationID, owner)
		return err
	case err != nil:
		return err
	}
	ts, terr := time.Parse("2006-01-02T15:04:05.999Z", updated)
	if terr != nil || time.Since(ts) > lockStaleAfter {
		_, err := s.db.Exec(`UPDATE dest_locks SET owner=?,
			updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE destination_id=?`,
			owner, destinationID)
		return err
	}
	return fmt.Errorf("destination %d held by %s (heartbeat %s ago)",
		destinationID, cur, time.Since(ts).Round(time.Second))
}

// Heartbeat refreshes an owned lock.
func (s *Store) Heartbeat(destinationID int64, owner string) error {
	res, err := s.db.Exec(`UPDATE dest_locks SET
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE destination_id=? AND owner=?`, destinationID, owner)
	if err != nil {
		return err
	}
	return expectOne(res, destinationID, "heartbeat (lock lost)")
}

// ReleaseLock drops an owned lock.
func (s *Store) ReleaseLock(destinationID int64, owner string) error {
	_, err := s.db.Exec(`DELETE FROM dest_locks
		WHERE destination_id=? AND owner=?`, destinationID, owner)
	return err
}

// ownerString builds a lock owner token (used by the M2-3 executor).
func ownerString() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "local"
	}
	return fmt.Sprintf("%s:%d:%d", host, os.Getpid(), time.Now().UnixNano())
}
