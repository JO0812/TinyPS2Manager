package queue

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/transfer"
	"github.com/jo/TinyPS2Manager/internal/usbextreme"
)

// fakeDisk is an in-memory Disk with timing, faults, and concurrency
// tracking for invariant tests.
type fakeDisk struct {
	mu            sync.Mutex
	files         map[string][]byte
	dirs          map[string]bool
	delay         time.Duration
	fail          func(path string) error
	curConcurrent int
	maxConcurrent int
	calls         atomic.Int64
	aborts        []string
	order         []string
}

func newFakeDisk(delay time.Duration) *fakeDisk {
	return &fakeDisk{files: map[string][]byte{}, dirs: map[string]bool{}, delay: delay}
}

func (d *fakeDisk) enter() func() {
	d.mu.Lock()
	d.curConcurrent++
	if d.curConcurrent > d.maxConcurrent {
		d.maxConcurrent = d.curConcurrent
	}
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		d.curConcurrent--
		d.mu.Unlock()
	}
}

func (d *fakeDisk) MkdirAll(path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dirs[path] = true
	return nil
}

func (d *fakeDisk) CopyToDest(ctx context.Context, finalPath string, src io.Reader, size int64, onProgress func(int64)) error {
	leave := d.enter()
	defer leave()
	select {
	case <-ctx.Done():
		d.mu.Lock()
		d.aborts = append(d.aborts, finalPath)
		d.mu.Unlock()
		return ctx.Err()
	case <-time.After(d.delay):
	}
	d.mu.Lock()
	fail := d.fail
	d.mu.Unlock()
	if fail != nil {
		if err := fail(finalPath); err != nil {
			return err
		}
	}
	data, err := io.ReadAll(io.LimitReader(src, size+1))
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return errors.New("fakeDisk: size mismatch")
	}
	if onProgress != nil {
		onProgress(size)
	}
	d.mu.Lock()
	d.files[finalPath] = data
	d.order = append(d.order, finalPath)
	d.mu.Unlock()
	d.calls.Add(1)
	return nil
}

type fakeStaged struct {
	disk  *fakeDisk
	final string
	buf   bytes.Buffer
	done  bool
}

func (d *fakeDisk) StageFile(dir, finalName string) (transfer.TempFile, error) {
	d.mu.Lock()
	d.dirs[dir] = true
	fail := d.fail
	d.mu.Unlock()
	if fail != nil {
		if err := fail(finalName); err != nil {
			return nil, err
		}
	}
	return &fakeStaged{disk: d, final: filepath.Join(dir, finalName)}, nil
}

func (s *fakeStaged) Write(p []byte) (int, error) { return s.buf.Write(p) }

func (s *fakeStaged) Commit() error {
	leave := s.disk.enter()
	defer leave()
	select {
	case <-time.After(s.disk.delay):
	default:
	}
	s.disk.mu.Lock()
	defer s.disk.mu.Unlock()
	s.disk.files[s.final] = append([]byte(nil), s.buf.Bytes()...)
	s.disk.order = append(s.disk.order, s.final)
	s.done = true
	s.disk.calls.Add(1)
	return nil
}

func (s *fakeStaged) Abort() { s.done = true }

func (d *fakeDisk) Remove(path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.files[path]; !ok {
		return os.ErrNotExist
	}
	delete(d.files, path)
	return nil
}

type fakeFileInfo struct{ size int64 }

func (f fakeFileInfo) Name() string       { return "" }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o644 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

func (d *fakeDisk) Stat(path string) (os.FileInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, ok := d.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return fakeFileInfo{size: int64(len(b))}, nil
}

func (d *fakeDisk) Open(path string) (io.ReadCloser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, ok := d.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), b...))), nil
}

func (d *fakeDisk) SweepStaleTemps(root string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for p := range d.files {
		if strings.Contains(filepath.Base(p), ".oplbm.") {
			delete(d.files, p)
			n++
		}
	}
	return n, nil
}

// --- Harness ---

type harness struct {
	qstore *Store
	lib    *library.Store
	dest   Destination
}

func newHarness(t *testing.T, dbPath string) *harness {
	t.Helper()
	qs, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { qs.Close() })
	libPath := dbPath + ".lib"
	if dbPath == ":memory:" {
		libPath = ":memory:"
	}
	ls, err := library.Open(libPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ls.Close() })
	d, err := qs.AddDestination(Destination{Path: t.TempDir(), Kind: DestFolder,
		Filesystem: "exfat"})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{qstore: qs, lib: ls, dest: d}
}

func writeRandom(t *testing.T, path string, n int64) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

func addISOItem(t *testing.T, h *harness, path, title string, dt library.DiscType) library.LibraryItem {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	it, err := h.lib.UpsertItem(library.LibraryItem{
		SourcePath: path, ContentHash: "h-" + path, Platform: library.PlatformPS2,
		Title: title, SizeBytes: fi.Size(), Status: library.StatusNew,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.lib.UpdateDetection(it.ID, dt, library.MethodHeuristic); err != nil {
		t.Fatal(err)
	}
	it.DiscType, it.DetectionMethod = dt, library.MethodHeuristic
	return it
}

func waitJobs(t *testing.T, h *harness, want JobStatus, timeout time.Duration) []Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		jobs, err := h.qstore.ListJobs()
		if err != nil {
			t.Fatal(err)
		}
		ok := len(jobs) > 0
		for _, j := range jobs {
			if j.Status != want {
				ok = false
			}
		}
		if ok {
			return jobs
		}
		if time.Now().After(deadline) {
			t.Fatalf("jobs not %q in time: %+v", want, jobs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitJobStatus(t *testing.T, h *harness, id int64, want JobStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		j, err := h.qstore.GetJob(id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d is %q, want %q", id, j.Status, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// --- Tests ---

func TestExecutorSequentialCopies(t *testing.T) {
	h := newHarness(t, ":memory:")
	fake := newFakeDisk(30 * time.Millisecond)
	srcDir := t.TempDir()
	var contents [][]byte
	var ids []int64
	for i := 0; i < 3; i++ {
		p := filepath.Join(srcDir, string(rune('a'+i))+".iso")
		contents = append(contents, writeRandom(t, p, 1<<20))
		it := addISOItem(t, h, p, "Game", library.DiscDVD)
		ids = append(ids, it.ID)
	}
	if _, err := h.qstore.Enqueue([]Job{
		{LibraryItemID: ids[0], DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
		{LibraryItemID: ids[1], DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
		{LibraryItemID: ids[2], DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
	}); err != nil {
		t.Fatal(err)
	}
	ex := New(h.qstore, h.lib, fake, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- ex.Run(ctx) }()
	jobs := waitJobs(t, h, JobDone, 15*time.Second)
	cancel()
	<-runErr
	if fake.maxConcurrent != 1 {
		t.Errorf("max concurrent writes = %d, want exactly 1", fake.maxConcurrent)
	}
	if fake.calls.Load() != 3 {
		t.Errorf("write calls = %d, want 3", fake.calls.Load())
	}
	// Completion order matches queue order.
	for i, j := range jobs {
		want := filepath.Join(h.dest.Path, "DVD", string(rune('a'+i))+".iso")
		if fake.order[i] != want {
			t.Errorf("completion %d = %s, want %s", i, fake.order[i], want)
		}
		if j.Phase != "" {
			t.Errorf("job %d phase = %q after done", j.ID, j.Phase)
		}
	}
	for i, want := range contents {
		got := fake.files[filepath.Join(h.dest.Path, "DVD", string(rune('a'+i))+".iso")]
		if !bytes.Equal(got, want) {
			t.Errorf("content %d mismatch", i)
		}
	}
}

func TestExecutorSecondExecutorRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.db")
	h := newHarness(t, path)
	fake := newFakeDisk(300 * time.Millisecond)
	p := filepath.Join(t.TempDir(), "g.iso")
	writeRandom(t, p, 1<<20)
	it := addISOItem(t, h, p, "Game", library.DiscDVD)
	if _, err := h.qstore.Enqueue([]Job{
		{LibraryItemID: it.ID, DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
		{LibraryItemID: it.ID, DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
	}); err != nil {
		t.Fatal(err)
	}
	qs2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer qs2.Close()
	exA := New(h.qstore, h.lib, fake, "")
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	go exA.Run(ctxA)
	time.Sleep(100 * time.Millisecond) // A holds the lease
	exB := New(qs2, h.lib, fake, "")
	ctxB, cancelB := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelB()
	if err := exB.RunDestination(ctxB, h.dest.ID); err == nil {
		t.Fatal("second executor: expected lock refusal")
	} else if !strings.Contains(err.Error(), "held by") {
		t.Fatalf("unexpected error: %v", err)
	}
	waitJobs(t, h, JobDone, 15*time.Second)
	if fake.calls.Load() != 2 {
		t.Errorf("write calls = %d, want exactly 2 (no double-execution)", fake.calls.Load())
	}
}

func TestExecutorErrorPausesAndCleans(t *testing.T) {
	h := newHarness(t, ":memory:")
	fake := newFakeDisk(10 * time.Millisecond)
	fake.fail = func(path string) error {
		if strings.Contains(path, "bad") {
			return errors.New("injected fault")
		}
		return nil
	}
	srcDir := t.TempDir()
	good := writeRandom(t, filepath.Join(srcDir, "good.iso"), 1<<20)
	writeRandom(t, filepath.Join(srcDir, "bad.iso"), 1<<20)
	gi := addISOItem(t, h, filepath.Join(srcDir, "good.iso"), "Good", library.DiscDVD)
	bi := addISOItem(t, h, filepath.Join(srcDir, "bad.iso"), "Bad", library.DiscDVD)
	jobs, err := h.qstore.Enqueue([]Job{
		{LibraryItemID: gi.ID, DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
		{LibraryItemID: bi.ID, DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	ex := New(h.qstore, h.lib, fake, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ex.Run(ctx)
	waitJobStatus(t, h, jobs[1].ID, JobError, 15*time.Second)
	waitJobStatus(t, h, jobs[0].ID, JobDone, 15*time.Second)
	if paused, _ := h.qstore.Paused(); !paused {
		t.Error("queue should auto-pause on job error")
	}
	bad, _ := h.qstore.GetJob(jobs[1].ID)
	if !strings.Contains(bad.Error, "injected fault") {
		t.Errorf("error = %q", bad.Error)
	}
	if _, ok := fake.files[filepath.Join(h.dest.Path, "DVD", "bad.iso")]; ok {
		t.Error("partial output survives failure")
	}
	// Fix, retry, resume: the failed job completes.
	fake.fail = nil
	if err := h.qstore.RetryJob(jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := h.qstore.SetPaused(false); err != nil {
		t.Fatal(err)
	}
	waitJobs(t, h, JobDone, 15*time.Second)
	if got := fake.files[filepath.Join(h.dest.Path, "DVD", "good.iso")]; !bytes.Equal(got, good) {
		t.Error("good content mismatch")
	}
}

func TestExecutorCancelRequeues(t *testing.T) {
	h := newHarness(t, ":memory:")
	fake := newFakeDisk(300 * time.Millisecond)
	srcDir := t.TempDir()
	writeRandom(t, filepath.Join(srcDir, "a.iso"), 1<<20)
	writeRandom(t, filepath.Join(srcDir, "b.iso"), 1<<20)
	ai := addISOItem(t, h, filepath.Join(srcDir, "a.iso"), "A", library.DiscDVD)
	bi := addISOItem(t, h, filepath.Join(srcDir, "b.iso"), "B", library.DiscDVD)
	jobs, err := h.qstore.Enqueue([]Job{
		{LibraryItemID: ai.ID, DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
		{LibraryItemID: bi.ID, DestinationID: h.dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	ex := New(h.qstore, h.lib, fake, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ex.Run(ctx)
	waitJobStatus(t, h, jobs[0].ID, JobRunning, 15*time.Second)
	if !ex.CancelDestination(h.dest.ID) {
		t.Fatal("CancelDestination reported nothing running")
	}
	// The abort is proven by effects, not by catching the transient
	// pending state (the loop re-claims instantly): the path lands in
	// the abort log and both jobs still complete with correct content.
	waitJobs(t, h, JobDone, 15*time.Second)
	aborted := false
	for _, p := range fake.aborts {
		if p == filepath.Join(h.dest.Path, "DVD", "a.iso") {
			aborted = true
		}
	}
	if !aborted {
		t.Errorf("no abort recorded for the cancelled write: %v", fake.aborts)
	}
	if ex.CancelDestination(9999) {
		t.Error("cancel on idle destination reported work")
	}
	waitJobs(t, h, JobDone, 15*time.Second)
	if fake.calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 (aborted write must not count)", fake.calls.Load())
	}
	aBack := fake.files[filepath.Join(h.dest.Path, "DVD", "a.iso")]
	aOrig, _ := os.ReadFile(filepath.Join(srcDir, "a.iso"))
	if !bytes.Equal(aBack, aOrig) {
		t.Error("post-abort content mismatch")
	}
}

// --- Real-disk end-to-ends (content correctness through FileDisk) ---

func TestExecutorConvertGrouped(t *testing.T) {
	h := newHarness(t, ":memory:")
	srcDir := t.TempDir()
	for i := 1; i <= 2; i++ {
		bin := makePatternedSectors(4)
		binName := filepath.Join(srcDir, "Game (Disc "+string(rune('0'+i))+").bin")
		cueName := filepath.Join(srcDir, "Game (Disc "+string(rune('0'+i))+").cue")
		if err := os.WriteFile(binName, bin, 0o644); err != nil {
			t.Fatal(err)
		}
		sheet := "FILE \"" + filepath.Base(binName) + "\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"
		if err := os.WriteFile(cueName, []byte(sheet), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := int64(1)
	var ids []int64
	for i := 1; i <= 2; i++ {
		title := "Game (Disc " + string(rune('0'+i)) + ")"
		cue := filepath.Join(srcDir, title+".cue")
		it, err := h.lib.UpsertItem(library.LibraryItem{
			SourcePath: cue, ContentHash: "c" + title, Platform: library.PlatformPS1,
			Title: title, DiscIndex: i, DiscGroupID: &g,
			SizeBytes: int64(4 * 2352), Status: library.StatusNew,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	if _, err := h.qstore.Enqueue([]Job{
		{LibraryItemID: ids[0], DestinationID: h.dest.ID, Kind: KindConvertCopy},
		{LibraryItemID: ids[1], DestinationID: h.dest.ID, Kind: KindConvertCopy},
	}); err != nil {
		t.Fatal(err)
	}
	ex := New(h.qstore, h.lib, transfer.FileDisk{}, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ex.Run(ctx)
	waitJobs(t, h, JobDone, 15*time.Second)
	cancel()
	pops := filepath.Join(h.dest.Path, "POPS")
	for i := 1; i <= 2; i++ {
		vcd := filepath.Join(pops, "Game (Disc "+string(rune('0'+i))+").VCD")
		got, err := os.ReadFile(vcd)
		if err != nil {
			t.Fatalf("read %s: %v", vcd, err)
		}
		if !bytes.Equal(got, makePatternedSectors(4)) {
			t.Errorf("%s content mismatch", vcd)
		}
		folder := filepath.Join(pops, "Game (Disc "+string(rune('0'+i))+")")
		discs, err := os.ReadFile(filepath.Join(folder, "DISCS.TXT"))
		if err != nil {
			t.Fatal(err)
		}
		want := "Game (Disc 1).VCD\nGame (Disc 2).VCD\n"
		if string(discs) != want {
			t.Errorf("DISCS.TXT = %q, want %q", discs, want)
		}
		vmc, err := os.ReadFile(filepath.Join(folder, "VMCDIR.TXT"))
		if err != nil {
			t.Fatal(err)
		}
		if string(vmc) != "Game (Disc 1)\n" {
			t.Errorf("VMCDIR.TXT = %q", vmc)
		}
	}
}

func makePatternedSectors(n int) []byte {
	out := make([]byte, 0, n*2352)
	for i := 0; i < n; i++ {
		for j := 0; j < 2352; j++ {
			out = append(out, byte(i+1))
		}
	}
	return out
}

func TestExecutorSplit(t *testing.T) {
	h := newHarness(t, ":memory:")
	srcDir := t.TempDir()
	iso := filepath.Join(srcDir, "dvd.iso")
	if err := os.WriteFile(iso, makeSerialISO(1000, "SPLT_001.01"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(iso)
	it, err := h.lib.UpsertItem(library.LibraryItem{
		SourcePath: iso, ContentHash: "split1", Platform: library.PlatformPS2,
		Title: "Split Game", DiscType: library.DiscDVD,
		DetectionMethod: library.MethodInspected,
		SizeBytes:       fi.Size(), Status: library.StatusNew,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.qstore.Enqueue([]Job{
		{LibraryItemID: it.ID, DestinationID: h.dest.ID, Kind: KindSplitAndCopy, BytesTotal: fi.Size()},
	}); err != nil {
		t.Fatal(err)
	}
	ex := New(h.qstore, h.lib, transfer.FileDisk{}, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ex.Run(ctx)
	waitJobs(t, h, JobDone, 15*time.Second)
	cancel()
	entries, err := usbextreme.List(h.dest.Path)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ul.cfg = %v, %v", entries, err)
	}
	if entries[0].Serial != "SPLT_001.01" || entries[0].Chunks != 1 {
		t.Errorf("entry = %+v", entries[0])
	}
	r, total, err := usbextreme.Open(h.dest.Path, "SPLT_001.01")
	if err != nil {
		t.Fatal(err)
	}
	back, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	orig, err := os.ReadFile(iso)
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(len(orig)) || !bytes.Equal(back, orig) {
		t.Error("split round-trip mismatch")
	}
}

func TestExecutorRestartRecovery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "q.db")
	libPath := dbPath + ".lib"
	qs, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := library.Open(libPath)
	if err != nil {
		t.Fatal(err)
	}
	dest, err := qs.AddDestination(Destination{Path: t.TempDir(), Kind: DestFolder})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "g.iso")
	content := writeRandom(t, p, 1<<20)
	it, err := ls.UpsertItem(library.LibraryItem{
		SourcePath: p, ContentHash: "rk", Platform: library.PlatformPS2,
		Title: "G", SizeBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Detection columns persist via UpdateDetection, not UpsertItem.
	if err := ls.UpdateDetection(it.ID, library.DiscDVD, library.MethodHeuristic); err != nil {
		t.Fatal(err)
	}
	jobs, err := qs.Enqueue([]Job{
		{LibraryItemID: it.ID, DestinationID: dest.ID, Kind: KindCopy, BytesTotal: 1 << 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the crash: claim the job (running, no executor), orphan a temp.
	if _, ok, err := qs.NextPending(dest.ID); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	stale := filepath.Join(dest.Path, "DVD", ".oplbm.deadbeef")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	qs.Close()
	ls.Close()

	// Relaunch: recovery resets + sweeps, then the queue completes.
	qs2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer qs2.Close()
	ls2, err := library.Open(libPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ls2.Close()
	h2 := &harness{qstore: qs2, lib: ls2, dest: dest}
	ex := New(qs2, ls2, transfer.FileDisk{}, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ex.Run(ctx)
	waitJobStatus(t, h2, jobs[0].ID, JobDone, 15*time.Second)
	cancel()
	if _, serr := os.Stat(stale); !os.IsNotExist(serr) {
		t.Error("stale temp survives restart")
	}
	back, err := os.ReadFile(filepath.Join(dest.Path, "DVD", "g.iso"))
	if err != nil || !bytes.Equal(back, content) {
		t.Error("post-restart content mismatch")
	}
}

func TestGroupedSerialLessNamesDistinct(t *testing.T) {
	// Regression: two serial-less discs sharing a group title must not
	// collapse to one VCD filename (silent overwrite). Each keeps its
	// disc index and the manifest lists both.
	h := newHarness(t, ":memory:")
	srcDir := t.TempDir()
	for i := 1; i <= 2; i++ {
		bin := makePatternedSectors(2)
		if err := os.WriteFile(filepath.Join(srcDir, "d.bin"), bin, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g := int64(1)
	var ids []int64
	for i := 1; i <= 2; i++ {
		binName := "d.bin"
		cueName := filepath.Join(srcDir, "Epic"+string(rune('0'+i))+".cue")
		sheet := "FILE \"" + binName + "\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"
		if err := os.WriteFile(cueName, []byte(sheet), 0o644); err != nil {
			t.Fatal(err)
		}
		it, err := h.lib.UpsertItem(library.LibraryItem{
			SourcePath: cueName, ContentHash: "sg" + string(rune('0'+i)),
			Platform: library.PlatformPS1, Title: "Epic", DiscIndex: i,
			DiscGroupID: &g, SizeBytes: int64(2 * 2352), Status: library.StatusNew,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	dest := h.dest
	pv1, err := PreviewConvert(mustGetItem(t, h, ids[0]), &dest, h.lib)
	if err != nil {
		t.Fatalf("PreviewConvert: %v", err)
	}
	pv2, err := PreviewConvert(mustGetItem(t, h, ids[1]), &dest, h.lib)
	if err != nil {
		t.Fatalf("PreviewConvert: %v", err)
	}
	if pv1.VCDPath == pv2.VCDPath {
		t.Fatalf("both discs map to %q", pv1.VCDPath)
	}
	if !strings.Contains(pv1.VCDPath, "Epic (Disc 1).VCD") {
		t.Errorf("disc 1 vcd = %q", pv1.VCDPath)
	}
	if !strings.Contains(pv2.VCDPath, "Epic (Disc 2).VCD") {
		t.Errorf("disc 2 vcd = %q", pv2.VCDPath)
	}
	if len(pv1.Manifests) != 4 {
		t.Errorf("manifests = %d, want 4 (2 per disc folder)", len(pv1.Manifests))
	}
}

func mustGetItem(t *testing.T, h *harness, id int64) *library.LibraryItem {
	t.Helper()
	it, err := h.lib.Get(id)
	if err != nil || it == nil {
		t.Fatalf("Get(%d) = %+v, %v", id, it, err)
	}
	return it
}

func TestEstimate(t *testing.T) {
	h := newHarness(t, ":memory:")
	fat := h.dest
	fat.Filesystem = "fat32"
	srcDir := t.TempDir()

	small := filepath.Join(srcDir, "cd.iso")
	writeRandom(t, small, 100)
	cdItem, _ := h.lib.UpsertItem(library.LibraryItem{SourcePath: small,
		ContentHash: "e1", Platform: library.PlatformPS2, Title: "CD",
		DiscType: library.DiscCD, SizeBytes: 100})
	kind, total, err := Estimate(&cdItem, &fat, h.lib)
	if err != nil || kind != KindCopy || total != 100 {
		t.Errorf("cd = %q,%d,%v", kind, total, err)
	}

	// 5 GiB sparse ISO with a serial: split-and-copy on FAT32.
	big := filepath.Join(srcDir, "dvd.iso")
	if err := os.WriteFile(big, makeSerialISO(6000000, "BIGG_001.01"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(big, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(5 << 30); err != nil {
		t.Fatal(err)
	}
	f.Close()
	fi, _ := os.Stat(big)
	dvdItem, _ := h.lib.UpsertItem(library.LibraryItem{SourcePath: big,
		ContentHash: "e2", Platform: library.PlatformPS2, Title: "DVD",
		DiscType: library.DiscDVD, SizeBytes: fi.Size()})
	kind, total, err = Estimate(&dvdItem, &fat, h.lib)
	if err != nil || kind != KindSplitAndCopy || total != 5<<30 {
		t.Errorf("dvd = %q,%d,%v", kind, total, err)
	}
	// Same image on exFAT stays a plain copy.
	exfat := fat
	exfat.Filesystem = "exfat"
	if kind, _, err := Estimate(&dvdItem, &exfat, h.lib); err != nil || kind != KindCopy {
		t.Errorf("exfat = %q,%v", kind, err)
	}

	// PS1 cue: convert with merge total.
	bin := makePatternedSectors(6)
	if err := os.WriteFile(filepath.Join(srcDir, "g.bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	cue := "FILE \"g.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n" +
		"  TRACK 02 AUDIO\n    INDEX 00 00:00:04\n    INDEX 01 00:00:05\n"
	if err := os.WriteFile(filepath.Join(srcDir, "g.cue"), []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	psItem, _ := h.lib.UpsertItem(library.LibraryItem{
		SourcePath: filepath.Join(srcDir, "g.cue"), ContentHash: "e3",
		Platform: library.PlatformPS1, Title: "G", SizeBytes: int64(len(bin))})
	kind, total, err = Estimate(&psItem, &fat, h.lib)
	if err != nil || kind != KindConvertCopy || total != 6*2352 {
		t.Errorf("ps1 = %q,%d,%v", kind, total, err)
	}

	// Unset disc type and unknown platform fail closed.
	unset, _ := h.lib.UpsertItem(library.LibraryItem{SourcePath: small,
		ContentHash: "e4", Platform: library.PlatformPS2, Title: "U", SizeBytes: 100})
	if _, _, err := Estimate(&unset, &fat, h.lib); err == nil {
		t.Error("unset type: expected error")
	}
	bogus := unset
	bogus.Platform = "ps3"
	if _, _, err := Estimate(&bogus, &fat, h.lib); err == nil {
		t.Error("bad platform: expected error")
	}
}

// makeSerialISO builds a tiny ISO with a PVD claiming sectors and a
// SYSTEM.CNF carrying serial (test-local; mirrors the M1 builders).
func makeSerialISO(sectors uint32, serial string) []byte {
	const ss = 2048
	img := make([]byte, 16*ss)
	pvd := make([]byte, ss)
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	copy(pvd[40:72], "TESTVOL")
	binary.LittleEndian.PutUint32(pvd[80:84], sectors)
	copy(pvd[156:], rec(nil, 17, ss, 2, []byte{0}))
	img = append(img, pvd...)
	root := rec(nil, 17, ss, 2, []byte{0})
	root = rec(root, 17, ss, 2, []byte{1})
	cnf := "BOOT = cdrom:\\" + serial + ";1\n"
	root = rec(root, 18, uint32(len(cnf)), 0, []byte("SYSTEM.CNF;1"))
	sec := make([]byte, ss)
	copy(sec, root)
	img = append(img, sec...)
	dat := make([]byte, ss)
	copy(dat, cnf)
	img = append(img, dat...)
	return img
}

func rec(buf []byte, extent, size uint32, flags byte, name []byte) []byte {
	recLen := 33 + len(name)
	if len(name)%2 == 0 {
		recLen++
	}
	r := make([]byte, recLen)
	r[0] = byte(recLen)
	binary.LittleEndian.PutUint32(r[2:6], extent)
	binary.LittleEndian.PutUint32(r[10:14], size)
	r[25] = flags
	r[32] = byte(len(name))
	copy(r[33:], name)
	return append(buf, r...)
}
