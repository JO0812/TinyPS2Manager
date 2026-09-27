package queue

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestDestinationSettings(t *testing.T) {
	st := openTestStore(t)
	if err := st.UpsertSettings(DestinationSettings{Path: "/mnt/ps2", Kind: DestFolder,
		FSOverride: "exfat", BDMPrefix: "OPL"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := st.GetSettings("/mnt/ps2")
	if err != nil || got == nil {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if got.Kind != DestFolder || got.FSOverride != "exfat" || got.BDMPrefix != "OPL" {
		t.Errorf("round-trip = %+v", got)
	}
	// Upsert overwrites.
	if err := st.UpsertSettings(DestinationSettings{Path: "/mnt/ps2", Kind: DestDrive}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetSettings("/mnt/ps2")
	if got.Kind != DestDrive || got.FSOverride != "" || got.BDMPrefix != "" {
		t.Errorf("overwrite = %+v", got)
	}
	// Missing path yields nil, not an error.
	if missing, err := st.GetSettings("/nope"); err != nil || missing != nil {
		t.Errorf("missing = %+v, %v", missing, err)
	}
	list, err := st.ListSettings()
	if err != nil || len(list) != 1 || list[0].Path != "/mnt/ps2" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if err := st.UpsertSettings(DestinationSettings{Kind: DestFolder}); err == nil {
		t.Error("empty path: expected error")
	}
	if err := st.UpsertSettings(DestinationSettings{Path: "/x", Kind: "tape"}); err == nil {
		t.Error("bad kind: expected error")
	}
	if err := st.UpsertSettings(DestinationSettings{Path: "/x", FSOverride: "ntfs"}); err == nil {
		t.Error("bad override: expected error")
	}
}

func TestMigrateLiveDestinationsLegacy(t *testing.T) {
	// A database shaped by the pre-live schema (destinations registry +
	// jobs.destination_id) must migrate to settings + destination_path,
	// and every subsequent Open must succeed (the old 001 file used to
	// resurrect dropped objects, breaking re-opens).
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE destinations (id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'folder',
			filesystem TEXT NOT NULL DEFAULT 'unknown', fs_override TEXT NOT NULL DEFAULT '',
			bdm_prefix TEXT NOT NULL DEFAULT '', free_bytes INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE jobs (id INTEGER PRIMARY KEY AUTOINCREMENT,
			library_item_id INTEGER NOT NULL, destination_id INTEGER NOT NULL REFERENCES destinations(id),
			kind TEXT NOT NULL, "order" INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
			phase TEXT NOT NULL DEFAULT '', bytes_total INTEGER NOT NULL DEFAULT 0,
			bytes_done INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX idx_jobs_dest_status ON jobs(destination_id, status)`,
		`INSERT INTO destinations(id, path, kind, fs_override, bdm_prefix) VALUES (7, '/mnt/old', 'drive', 'fat32', 'OPL')`,
		`INSERT INTO jobs(id, library_item_id, destination_id, kind, "order", status) VALUES (9, 1, 7, 'copy', 0, 'pending')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("legacy fixture: %v", err)
		}
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := st.GetSettings("/mnt/old")
	if err != nil || got == nil {
		t.Fatalf("settings = %+v, %v", got, err)
	}
	if got.Kind != DestDrive || got.FSOverride != "fat32" || got.BDMPrefix != "OPL" {
		t.Errorf("backfilled settings = %+v", got)
	}
	j, err := st.GetJob(9)
	if err != nil || j == nil || j.DestinationPath != "/mnt/old" {
		t.Fatalf("migrated job = %+v, %v", j, err)
	}
	if hasTable(st.db, "destinations") {
		t.Error("destinations table survives migration")
	}
	st.Close()
	// Re-open must succeed (regression: resurrected legacy objects broke it).
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer st2.Close()
	if j2, _ := st2.GetJob(9); j2 == nil || j2.DestinationPath != "/mnt/old" {
		t.Errorf("job after re-open = %+v", j2)
	}
}

func TestDeleteSettings(t *testing.T) {
	st := openTestStore(t)
	if err := st.UpsertSettings(DestinationSettings{Path: "/mnt/gone", Kind: DestDrive}); err != nil {
		t.Fatal(err)
	}
	// Refused while jobs reference the path.
	if _, err := st.Enqueue([]Job{{LibraryItemID: 1, DestinationPath: "/mnt/gone", Kind: KindCopy}}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSettings("/mnt/gone"); !errors.Is(err, ErrDestinationHasJobs) {
		t.Fatalf("delete with jobs = %v, want ErrDestinationHasJobs", err)
	}
	// After cancelling the job, delete succeeds and the row is gone.
	jobs, _ := st.ListJobs()
	if err := st.CancelJob(jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSettings("/mnt/gone"); err != nil {
		t.Fatalf("delete = %v", err)
	}
	if got, _ := st.GetSettings("/mnt/gone"); got != nil {
		t.Errorf("deleted settings survive: %+v", got)
	}
	if err := st.DeleteSettings("/mnt/gone"); err == nil {
		t.Error("delete missing: expected error")
	}
}

func TestEnqueueOrderAndClaim(t *testing.T) {
	st := openTestStore(t)
	dPath := "/d"
	jobs, err := st.Enqueue([]Job{
		{LibraryItemID: 1, DestinationPath: dPath, Kind: KindCopy, BytesTotal: 10},
		{LibraryItemID: 2, DestinationPath: dPath, Kind: KindCopy, BytesTotal: 20},
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if jobs[0].Order != 0 || jobs[1].Order != 1 || jobs[0].Status != JobPending {
		t.Errorf("order/status = %+v", jobs)
	}
	first, ok, err := st.NextPending(dPath)
	if err != nil || !ok || first.ID != jobs[0].ID || first.Status != JobRunning {
		t.Fatalf("claim = %+v,%v,%v", first, ok, err)
	}
	// Second claim skips the running job and takes the next.
	second, ok, err := st.NextPending(dPath)
	if err != nil || !ok || second.ID != jobs[1].ID {
		t.Fatalf("claim2 = %+v,%v,%v", second, ok, err)
	}
	if _, ok, _ := st.NextPending(dPath); ok {
		t.Error("dry queue claimed a job")
	}
	if _, err := st.Enqueue([]Job{{Kind: "teleport"}}); err == nil {
		t.Error("bad kind: expected error")
	}
}

func TestRetryPauseResumeCancel(t *testing.T) {
	st := openTestStore(t)
	dPath := "/d"
	ids := func() []Job {
		j, _ := st.Enqueue([]Job{{LibraryItemID: 1, DestinationPath: dPath, Kind: KindCopy}})
		return j
	}
	j := ids()[0]
	if err := st.FinishJob(j.ID, "boom"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetJob(j.ID)
	if got.Status != JobError || got.Error != "boom" {
		t.Errorf("finish = %+v", got)
	}
	if n, err := st.IncrementAttempts(j.ID); err != nil || n != 1 {
		t.Fatalf("first attempt = %d, %v", n, err)
	}
	got, _ = st.GetJob(j.ID)
	if got.Attempts != 1 {
		t.Errorf("attempt count = %d, want 1", got.Attempts)
	}
	if err := st.RetryJob(j.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetJob(j.ID)
	if got.Status != JobPending || got.Error != "" || got.BytesDone != 0 || got.Attempts != 0 {
		t.Errorf("retry = %+v", got)
	}
	if err := st.RetryJob(j.ID); err == nil {
		t.Error("retry non-error: expected error")
	}
	if err := st.PauseJob(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.NextPending(dPath); ok {
		t.Error("paused job claimed")
	}
	if err := st.ResumeJob(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.NextPending(dPath); !ok {
		t.Error("resumed job not claimable")
	}
	if err := st.FinishJob(j.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetJob(j.ID)
	if got.Status != JobDone {
		t.Errorf("done = %+v", got)
	}
	j2 := ids()[0]
	if err := st.CancelJob(j2.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetJob(j2.ID); got != nil {
		t.Error("cancelled job survives")
	}
}

func TestReorder(t *testing.T) {
	st := openTestStore(t)
	dPath := "/d"
	j, _ := st.Enqueue([]Job{
		{LibraryItemID: 1, DestinationPath: dPath, Kind: KindCopy},
		{LibraryItemID: 2, DestinationPath: dPath, Kind: KindCopy},
		{LibraryItemID: 3, DestinationPath: dPath, Kind: KindCopy},
	})
	if err := st.Reorder(j[2].ID, 0); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListJobs()
	if list[0].ID != j[2].ID || list[1].ID != j[0].ID || list[2].ID != j[1].ID {
		t.Errorf("order = %v", list)
	}
	for i, got := range list {
		if got.Order != i {
			t.Errorf("job %d has order %d", got.ID, got.Order)
		}
	}
	if err := st.Reorder(9999, 0); err == nil {
		t.Error("reorder missing: expected error")
	}
}

func TestInFlightBytes(t *testing.T) {
	st := openTestStore(t)
	dPath := "/d"
	if _, err := st.Enqueue([]Job{
		{LibraryItemID: 1, DestinationPath: dPath, Kind: KindCopy, BytesTotal: 100},
		{LibraryItemID: 2, DestinationPath: dPath, Kind: KindCopy, BytesTotal: 50},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Checkpoint(1, "Copying", 40); err != nil {
		t.Fatal(err)
	}
	got, err := st.InFlightBytes(dPath)
	if err != nil || got != 110 { // (100-40) + 50
		t.Errorf("in-flight = %d, %v", got, err)
	}
}

func TestGlobalPause(t *testing.T) {
	st := openTestStore(t)
	if p, _ := st.Paused(); p {
		t.Error("default should be running")
	}
	if err := st.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.Paused(); !p {
		t.Error("should be paused")
	}
	if err := st.SetPaused(false); err != nil {
		t.Fatal(err)
	}
}

func TestDestLocks(t *testing.T) {
	// Locks must survive across connections: use a file DB.
	path := t.TempDir() + "/q.db"
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := a.AcquireLock("/locks/a", "owner-a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := b.AcquireLock("/locks/a", "owner-b"); err == nil {
		t.Error("double acquire: expected error")
	}
	if err := a.Heartbeat("/locks/a", "owner-a"); err != nil {
		t.Errorf("heartbeat: %v", err)
	}
	if err := a.Heartbeat("/locks/a", "impostor"); err == nil {
		t.Error("foreign heartbeat: expected error")
	}
	if err := a.ReleaseLock("/locks/a", "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := b.AcquireLock("/locks/a", "owner-b"); err != nil {
		t.Errorf("re-acquire after release: %v", err)
	}
}
