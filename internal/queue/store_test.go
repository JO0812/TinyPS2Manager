package queue

import (
	"testing"
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

func TestDestinationCRUD(t *testing.T) {
	st := openTestStore(t)
	d, err := st.AddDestination(Destination{Path: "/mnt/ps2", Kind: DestFolder,
		Filesystem: "exfat", BDMPrefix: "OPL", FreeBytes: 1 << 30})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if d.ID == 0 {
		t.Error("no ID assigned")
	}
	got, err := st.GetDestination(d.ID)
	if err != nil || got == nil || got.Path != "/mnt/ps2" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if got.EffectiveFilesystem() != "exfat" {
		t.Errorf("effective = %q", got.EffectiveFilesystem())
	}
	if err := st.UpdateDestinationPrefix(d.ID, "X"); err != nil {
		t.Fatal(err)
	}
	if err := st.RefreshDestinationStats(d.ID, "fat32", 99); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetDestination(d.ID)
	if got.BDMPrefix != "X" || got.Filesystem != "fat32" || got.FreeBytes != 99 {
		t.Errorf("refresh = %+v", got)
	}
	// Override wins over detection.
	od, _ := st.AddDestination(Destination{Path: "/mnt/u", Kind: DestDrive,
		Filesystem: "unknown", FSOverride: "fat32"})
	if od.EffectiveFilesystem() != "fat32" {
		t.Errorf("override not effective: %+v", od)
	}
	list, err := st.ListDestinations()
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
	if _, err := st.AddDestination(Destination{Kind: DestFolder}); err == nil {
		t.Error("empty path: expected error")
	}
	if _, err := st.AddDestination(Destination{Path: "/x", Kind: "tape"}); err == nil {
		t.Error("bad kind: expected error")
	}
}

func TestEnqueueOrderAndClaim(t *testing.T) {
	st := openTestStore(t)
	d, _ := st.AddDestination(Destination{Path: "/d", Kind: DestFolder})
	jobs, err := st.Enqueue([]Job{
		{LibraryItemID: 1, DestinationID: d.ID, Kind: KindCopy, BytesTotal: 10},
		{LibraryItemID: 2, DestinationID: d.ID, Kind: KindCopy, BytesTotal: 20},
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if jobs[0].Order != 0 || jobs[1].Order != 1 || jobs[0].Status != JobPending {
		t.Errorf("order/status = %+v", jobs)
	}
	first, ok, err := st.NextPending(d.ID)
	if err != nil || !ok || first.ID != jobs[0].ID || first.Status != JobRunning {
		t.Fatalf("claim = %+v,%v,%v", first, ok, err)
	}
	// Second claim skips the running job and takes the next.
	second, ok, err := st.NextPending(d.ID)
	if err != nil || !ok || second.ID != jobs[1].ID {
		t.Fatalf("claim2 = %+v,%v,%v", second, ok, err)
	}
	if _, ok, _ := st.NextPending(d.ID); ok {
		t.Error("dry queue claimed a job")
	}
	if _, err := st.Enqueue([]Job{{Kind: "teleport"}}); err == nil {
		t.Error("bad kind: expected error")
	}
}

func TestRetryPauseResumeCancel(t *testing.T) {
	st := openTestStore(t)
	d, _ := st.AddDestination(Destination{Path: "/d", Kind: DestFolder})
	ids := func() []Job {
		j, _ := st.Enqueue([]Job{{LibraryItemID: 1, DestinationID: d.ID, Kind: KindCopy}})
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
	if err := st.RetryJob(j.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetJob(j.ID)
	if got.Status != JobPending || got.Error != "" || got.BytesDone != 0 {
		t.Errorf("retry = %+v", got)
	}
	if err := st.RetryJob(j.ID); err == nil {
		t.Error("retry non-error: expected error")
	}
	if err := st.PauseJob(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.NextPending(d.ID); ok {
		t.Error("paused job claimed")
	}
	if err := st.ResumeJob(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.NextPending(d.ID); !ok {
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
	d, _ := st.AddDestination(Destination{Path: "/d", Kind: DestFolder})
	j, _ := st.Enqueue([]Job{
		{LibraryItemID: 1, DestinationID: d.ID, Kind: KindCopy},
		{LibraryItemID: 2, DestinationID: d.ID, Kind: KindCopy},
		{LibraryItemID: 3, DestinationID: d.ID, Kind: KindCopy},
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
	d, _ := st.AddDestination(Destination{Path: "/d", Kind: DestFolder})
	if _, err := st.Enqueue([]Job{
		{LibraryItemID: 1, DestinationID: d.ID, Kind: KindCopy, BytesTotal: 100},
		{LibraryItemID: 2, DestinationID: d.ID, Kind: KindCopy, BytesTotal: 50},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Checkpoint(1, "Copying", 40); err != nil {
		t.Fatal(err)
	}
	got, err := st.InFlightBytes(d.ID)
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
	if err := a.AcquireLock(7, "owner-a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := b.AcquireLock(7, "owner-b"); err == nil {
		t.Error("double acquire: expected error")
	}
	if err := a.Heartbeat(7, "owner-a"); err != nil {
		t.Errorf("heartbeat: %v", err)
	}
	if err := a.Heartbeat(7, "impostor"); err == nil {
		t.Error("foreign heartbeat: expected error")
	}
	if err := a.ReleaseLock(7, "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := b.AcquireLock(7, "owner-b"); err != nil {
		t.Errorf("re-acquire after release: %v", err)
	}
}
