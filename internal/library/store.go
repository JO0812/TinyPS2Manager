package library

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed migrations/001_init.sql
var migration001 string

// Store is the SQLite-backed library metadata store (pure Go, no cgo).
// Overrides are keyed by content hash so re-imports keep user choices
// (spec §2.3 #1).
type Store struct {
	db *sql.DB
}

// Open opens (creating) the database at path — ":memory:" for tests — and
// runs pending migrations. Shared-file discipline with the queue package:
// WAL mode (readers never block the single writer), a 5s busy timeout so
// cross-pool writers serialize instead of failing, and one connection.
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
		return nil, fmt.Errorf("migrate library schema: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO schema_version(version) VALUES (1)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("record schema version: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// UpsertItem inserts a scanned item, or refreshes the volatile fields of the
// existing row on content-hash conflict. Detection state (disc_type,
// detection_method) and lifecycle status are never clobbered, except that a
// row stuck in error is reopened as new on re-scan.
func (s *Store) UpsertItem(it LibraryItem) (LibraryItem, error) {
	existing, err := s.GetByHash(it.ContentHash)
	if err != nil {
		return LibraryItem{}, err
	}
	if existing == nil {
		res, err := s.db.Exec(`INSERT INTO library_items
			(source_path, content_hash, platform, title, disc_index, disc_group_id, size_bytes, status)
			VALUES (?,?,?,?,?,?,?,?)`,
			it.SourcePath, it.ContentHash, string(it.Platform), it.Title,
			it.DiscIndex, it.DiscGroupID, it.SizeBytes, string(it.Status))
		if err != nil {
			return LibraryItem{}, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return LibraryItem{}, err
		}
		it.ID = id
		return it, nil
	}
	status := existing.Status
	if status == StatusError {
		status = StatusNew
	}
	_, err = s.db.Exec(`UPDATE library_items SET
			source_path=?, title=?, disc_index=?, disc_group_id=?, size_bytes=?,
			status=?, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=?`,
		it.SourcePath, it.Title, it.DiscIndex, it.DiscGroupID,
		it.SizeBytes, string(status), existing.ID)
	if err != nil {
		return LibraryItem{}, err
	}
	existing.SourcePath, existing.Title = it.SourcePath, it.Title
	existing.DiscIndex, existing.DiscGroupID = it.DiscIndex, it.DiscGroupID
	existing.SizeBytes, existing.Status = it.SizeBytes, status
	return *existing, nil
}

// Get returns the item by ID, or nil.
func (s *Store) Get(id int64) (*LibraryItem, error) {
	row := s.db.QueryRow(`SELECT id, source_path, content_hash, platform,
		disc_type, detection_method, title, disc_index, disc_group_id,
		size_bytes, status FROM library_items WHERE id=?`, id)
	return scanItem(row)
}

// GetByHash returns the item with this content hash, or nil.
func (s *Store) GetByHash(hash string) (*LibraryItem, error) {
	row := s.db.QueryRow(`SELECT id, source_path, content_hash, platform,
		disc_type, detection_method, title, disc_index, disc_group_id,
		size_bytes, status FROM library_items WHERE content_hash=?`, hash)
	return scanItem(row)
}

// GetOverride returns the persisted user disc-type choice for a hash.
func (s *Store) GetOverride(hash string) (DiscType, bool, error) {
	it, err := s.GetByHash(hash)
	if err != nil || it == nil {
		return "", false, err
	}
	if it.DetectionMethod == MethodOverride && it.DiscType != "" {
		return it.DiscType, true, nil
	}
	return "", false, nil
}

// SetDiscOverride records an explicit user choice (spec §2.3 #1).
func (s *Store) SetDiscOverride(hash string, dt DiscType) error {
	if dt != DiscCD && dt != DiscDVD {
		return fmt.Errorf("bad disc type %q", dt)
	}
	res, err := s.db.Exec(`UPDATE library_items SET disc_type=?,
		detection_method=?, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE content_hash=?`, string(dt), string(MethodOverride), hash)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no library item with hash %s", hash)
	}
	return nil
}

// UpdateDetection records an automatic decision, never clobbering a user
// override.
func (s *Store) UpdateDetection(id int64, dt DiscType, m DetectionMethod) error {
	_, err := s.db.Exec(`UPDATE library_items SET disc_type=?,
		detection_method=?, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id=? AND detection_method != ?`,
		string(dt), string(m), id, string(MethodOverride))
	return err
}

// UpdateTitle renames an item.
func (s *Store) UpdateTitle(id int64, title string) error {
	if title == "" {
		return fmt.Errorf("title is empty")
	}
	res, err := s.db.Exec(`UPDATE library_items SET title=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, title, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no library item %d", id)
	}
	return nil
}

// SetGroup assigns (or, when nil, clears) a multi-disc group.
func (s *Store) SetGroup(id int64, groupID *int64) error {
	res, err := s.db.Exec(`UPDATE library_items SET disc_group_id=?,
		updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, groupID, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no library item %d", id)
	}
	return nil
}

// List returns all items in insertion order.
func (s *Store) List() ([]LibraryItem, error) {
	rows, err := s.db.Query(`SELECT id, source_path, content_hash, platform,
		disc_type, detection_method, title, disc_index, disc_group_id,
		size_bytes, status FROM library_items ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryItem
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// ListByGroupID returns all items sharing a multi-disc group, ordered by
// disc index.
func (s *Store) ListByGroupID(groupID int64) ([]LibraryItem, error) {
	rows, err := s.db.Query(`SELECT id, source_path, content_hash, platform,
		disc_type, detection_method, title, disc_index, disc_group_id,
		size_bytes, status FROM library_items WHERE disc_group_id=? ORDER BY disc_index, id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryItem
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// RemapGroup rewrites a temporary scan-time group ID to a real one
// (see ScanDir; the M1 CLI persist step allocates via NextGroupID).
func (s *Store) RemapGroup(old, new int64) error {
	_, err := s.db.Exec(`UPDATE library_items SET disc_group_id=?
		WHERE disc_group_id=?`, new, old)
	return err
}

// NextGroupID allocates a fresh multi-disc group ID (always >= 1; 0 and
// negatives are reserved for absent/temporary).
func (s *Store) NextGroupID() (int64, error) {
	var max sql.NullInt64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(disc_group_id),0)
		FROM library_items`).Scan(&max); err != nil {
		return 0, err
	}
	if id := max.Int64 + 1; id >= 1 {
		return id, nil
	}
	return 1, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanItem(r rowScanner) (*LibraryItem, error) {
	var it LibraryItem
	var platform, discType, method, status string
	var group sql.NullInt64
	err := r.Scan(&it.ID, &it.SourcePath, &it.ContentHash, &platform,
		&discType, &method, &it.Title, &it.DiscIndex, &group,
		&it.SizeBytes, &status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	it.Platform = Platform(platform)
	it.DiscType = DiscType(discType)
	it.DetectionMethod = DetectionMethod(method)
	it.Status = Status(status)
	if group.Valid {
		g := group.Int64
		it.DiscGroupID = &g
	}
	return &it, nil
}
