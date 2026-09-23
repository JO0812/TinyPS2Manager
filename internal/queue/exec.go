package queue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jo/TinyPS2Manager/internal/cuebin"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/transfer"
	"github.com/jo/TinyPS2Manager/internal/usbextreme"
)

// pollInterval is the idle cadence for dry/paused queues.
const pollInterval = 250 * time.Millisecond

// heartbeatInterval refreshes the cross-process destination lease.
const heartbeatInterval = 20 * time.Second

// Executor runs jobs sequentially per destination until ctx ends.
type Executor struct {
	store      *Store
	lib        *library.Store
	disk       transfer.Disk
	stagingDir string // reserved for future spillover (see package doc)

	mu      sync.Mutex
	cancels map[int64]context.CancelFunc // destID -> running job cancel
}

// New builds an Executor. disk performs all destination writes (real:
// transfer.FileDisk; tests: in-memory fake).
func New(store *Store, lib *library.Store, disk transfer.Disk, stagingDir string) *Executor {
	return &Executor{
		store: store, lib: lib, disk: disk, stagingDir: stagingDir,
		cancels: map[int64]context.CancelFunc{},
	}
}

// Run supervises one writer goroutine per destination until ctx ends,
// rescanning for newly added destinations. A failed runner (e.g. lease
// lost to another process) cools down 30s before respawn; shutdown waits
// for all runners.
func (e *Executor) Run(ctx context.Context) error {
	type runner struct {
		cancel context.CancelFunc
		done   chan error
	}
	runners := map[int64]*runner{}
	cooled := map[int64]time.Time{}
	spawn := func(id int64) {
		rctx, cancel := context.WithCancel(ctx)
		r := &runner{cancel: cancel, done: make(chan error, 1)}
		runners[id] = r
		go func() { r.done <- e.RunDestination(rctx, id) }()
	}
	scan := func() {
		dests, err := e.store.ListDestinations()
		if err != nil {
			return
		}
		for id, r := range runners {
			select {
			case err := <-r.done:
				delete(runners, id)
				// Returns while the supervisor lives are real failures
				// (lease/config), not shutdown: cool the destination.
				if err != nil && ctx.Err() == nil {
					cooled[id] = time.Now()
				}
			default:
			}
		}
		known := map[int64]bool{}
		for _, d := range dests {
			known[d.ID] = true
		}
		for id := range runners {
			if !known[id] {
				runners[id].cancel()
				delete(runners, id)
			}
		}
		for _, d := range dests {
			if _, ok := runners[d.ID]; ok {
				continue
			}
			if t, bad := cooled[d.ID]; bad && time.Since(t) < 30*time.Second {
				continue
			}
			spawn(d.ID)
		}
	}
	scan()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, r := range runners {
				r.cancel()
			}
			for _, r := range runners {
				<-r.done
			}
			return ctx.Err()
		case <-tick.C:
			scan()
		}
	}
}

// CancelDestination aborts the running job on a destination (partial output
// deleted, job requeued). False when nothing runs there.
func (e *Executor) CancelDestination(destID int64) bool {
	e.mu.Lock()
	cancel, ok := e.cancels[destID]
	e.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// RunDestination holds the destination lease and drains its queue until ctx
// ends: crash-recover, then claim → prepare-ahead → write → verify.
func (e *Executor) RunDestination(ctx context.Context, destID int64) error {
	dest, err := e.store.GetDestination(destID)
	if err != nil {
		return err
	}
	if dest == nil {
		return fmt.Errorf("no destination %d", destID)
	}
	owner := ownerString()
	if err := e.store.AcquireLock(destID, owner); err != nil {
		return err
	}
	defer e.store.ReleaseLock(destID, owner)

	if err := e.recover(destID); err != nil {
		return err
	}
	if _, err := e.disk.SweepStaleTemps(dest.Path); err != nil {
		return err
	}

	beat := time.NewTicker(heartbeatInterval)
	defer beat.Stop()
	go func() {
		for range beat.C {
			_ = e.store.Heartbeat(destID, owner)
		}
	}()

	var ahead *prepFuture
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if paused, err := e.store.Paused(); err != nil {
			return err
		} else if paused {
			time.Sleep(pollInterval)
			continue
		}
		job, ok, err := e.store.NextPending(destID)
		if err != nil {
			return err
		}
		if !ok {
			time.Sleep(pollInterval)
			continue
		}
		var outcome prepOutcome
		if ahead != nil && ahead.jobID == job.ID {
			outcome = awaitPrep(ctx, ahead)
			ahead = nil
		} else {
			ahead = nil
			w, err := e.prepare(job)
			outcome = prepOutcome{work: w, err: err}
		}
		// Queue the next preparation while this job writes.
		ahead = e.prepareAhead(ctx, destID, job.ID)
		if outcome.err != nil {
			e.fail(job, outcome.err, false)
			continue
		}
		e.executeJob(ctx, dest, job, outcome.work)
	}
}

// recover resets crash-stuck running jobs to pending (re-run from scratch
// after cleanup; see package doc).
func (e *Executor) recover(destID int64) error {
	jobs, err := e.store.ListJobs()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.DestinationID == destID && j.Status == JobRunning {
			if err := e.store.RequeueJob(j.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// prepFuture is a background preparation result.
type prepFuture struct {
	jobID int64
	done  chan prepOutcome
}

type prepOutcome struct {
	work workItem
	err  error
}

// prepareAhead peeks the next pending job (skipping the just-claimed one)
// and prepares it in the background. Nil when the queue is dry.
func (e *Executor) prepareAhead(ctx context.Context, destID, claimedID int64) *prepFuture {
	peek, ok, err := e.store.PeekPending(destID)
	if err != nil || !ok || peek.ID == claimedID {
		return nil
	}
	f := &prepFuture{jobID: peek.ID, done: make(chan prepOutcome, 1)}
	go func() {
		work, err := e.prepare(peek)
		select {
		case f.done <- prepOutcome{work: work, err: err}:
		case <-ctx.Done():
		}
	}()
	return f
}

func awaitPrep(ctx context.Context, f *prepFuture) prepOutcome {
	select {
	case out := <-f.done:
		return out
	case <-ctx.Done():
		return prepOutcome{err: ctx.Err()}
	}
}

// workItem is a fully computed job: everything below is IO.
type workItem struct {
	job   *Job
	item  *library.LibraryItem
	dest  *Destination
	copy  *copyPlan
	conv  *convertPlan
	split *splitPlan
}

// prepare loads the item + destination and computes the kind plan (pure CPU
// and small metadata reads; no destination writes).
func (e *Executor) prepare(job *Job) (workItem, error) {
	var w workItem
	item, err := e.lib.Get(job.LibraryItemID)
	if err != nil {
		return w, err
	}
	if item == nil {
		return w, fmt.Errorf("job %d: library item %d gone", job.ID, job.LibraryItemID)
	}
	dest, err := e.store.GetDestination(job.DestinationID)
	if err != nil {
		return w, err
	}
	if dest == nil {
		return w, fmt.Errorf("job %d: destination %d gone", job.ID, job.DestinationID)
	}
	w = workItem{job: job, item: item, dest: dest}
	switch job.Kind {
	case KindCopy:
		w.copy, err = planCopy(item, dest)
	case KindConvertCopy:
		w.conv, err = planConvert(item, dest, e.lib)
	case KindSplitAndCopy:
		w.split, err = planSplit(item, dest)
	case KindEmberCopy:
		err = fmt.Errorf("copy-ps1-ember executes in its own milestone")
	case KindEnrich:
		err = fmt.Errorf("enrich executes in its own milestone")
	default:
		err = fmt.Errorf("unknown job kind %q", job.Kind)
	}
	return w, err
}

// fail records a job error. Preparation failures (deterministic: bad
// sources, unreadable serials) park the job immediately — retrying cannot
// help. Write/verify failures (possibly transient I/O) auto-retry up to
// MaxAttempts, then park with auto-pause (spec §6.4 #7: never skip
// silently). Manual Retry always resets the counter.
func (e *Executor) fail(job *Job, err error, retryable bool) {
	attempts, aerr := e.store.IncrementAttempts(job.ID)
	if aerr != nil {
		_ = e.store.FinishJob(job.ID, err.Error())
		_ = e.store.SetPaused(true)
		return
	}
	if retryable && attempts < MaxAttempts {
		if rerr := e.store.RequeueJob(job.ID); rerr == nil {
			return
		}
	}
	_ = e.store.FinishJob(job.ID, fmt.Sprintf("%s (attempt %d of %d)", err.Error(), attempts, MaxAttempts))
	_ = e.store.SetPaused(true)
}

// executeJob runs one claimed job's write + verify phases, then finishes
// or fails it. Cancellation requeues with partials deleted (no pause).
func (e *Executor) executeJob(ctx context.Context, dest *Destination, job *Job, w workItem) {
	jobCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.cancels[dest.ID] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.cancels, dest.ID)
		e.mu.Unlock()
		cancel()
	}()

	tr := &tracker{store: e.store, id: job.ID, total: w.total()}
	var runErr error
	switch job.Kind {
	case KindCopy:
		runErr = e.runCopy(jobCtx, w, tr)
	case KindConvertCopy:
		runErr = e.runConvert(jobCtx, w, tr)
	case KindSplitAndCopy:
		runErr = e.runSplit(jobCtx, w, tr)
	default:
		runErr = fmt.Errorf("unknown job kind %q", job.Kind)
	}
	if runErr != nil {
		if jobCtx.Err() != nil && ctx.Err() == nil {
			// Targeted cancel (or parent done): requeue cleanly.
			_ = e.store.RequeueJob(job.ID)
			return
		}
		if ctx.Err() != nil {
			_ = e.store.RequeueJob(job.ID)
			return
		}
		e.fail(job, runErr, true)
		return
	}
	_ = e.store.FinishJob(job.ID, "")
}

func (w workItem) total() int64 {
	switch {
	case w.copy != nil:
		return w.copy.total
	case w.conv != nil:
		return w.conv.total
	case w.split != nil:
		return w.split.size
	}
	return 0
}

// tracker throttles progress checkpoints to ~1% or 1s (plan §4.2 #9:
// no SQLite write amplification on multi-GB copies).
type tracker struct {
	store   *Store
	id      int64
	phase   string
	total   int64
	done    int64
	lastPct int64
	last    time.Time
}

func (t *tracker) setPhase(phase string) {
	t.phase = phase
	_ = t.store.Checkpoint(t.id, phase, t.done)
}

func (t *tracker) add(n int64) {
	t.done += n
	if t.total <= 0 {
		return
	}
	pct := t.done * 100 / t.total
	now := time.Now()
	if pct > t.lastPct || now.Sub(t.last) >= time.Second {
		t.lastPct, t.last = pct, now
		_ = t.store.Checkpoint(t.id, t.phase, t.done)
	}
}

// runCopy streams the source file to its bucket and hash-verifies it.
func (e *Executor) runCopy(ctx context.Context, w workItem, tr *tracker) error {
	tr.setPhase(PhaseCopying)
	src, err := os.Open(w.copy.srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	srcHash := sha256.New()
	if err := e.disk.CopyToDest(ctx, w.copy.destPath,
		io.TeeReader(src, srcHash), w.copy.total, tr.add); err != nil {
		return err
	}
	return e.verifyFile(tr, w.copy.destPath, w.copy.total, srcHash.Sum(nil))
}

// tapWriter hashes and counts a generated stream (VCD builds).
type tapWriter struct {
	w  io.Writer
	h  hash.Hash
	tr *tracker
}

func (t *tapWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	t.h.Write(p[:n])
	t.tr.add(int64(n))
	return n, err
}

// runConvert builds the VCD straight into a same-volume temp (single
// allocation extent), commits it, writes identical manifests into every
// disc folder, then hash-verifies the committed file.
func (e *Executor) runConvert(ctx context.Context, w workItem, tr *tracker) error {
	c := w.conv
	if err := e.disk.MkdirAll(c.popsDir); err != nil {
		return err
	}
	for _, d := range c.vmcDirs {
		if err := e.disk.MkdirAll(d); err != nil {
			return err
		}
	}
	tr.setPhase(PhaseConverting)
	tmp, err := e.disk.StageFile(c.popsDir, c.vcdName)
	if err != nil {
		return err
	}
	vcdPath := filepath.Join(c.popsDir, c.vcdName)
	tap := &tapWriter{w: tmp, h: sha256.New(), tr: tr}
	if _, err := cuebin.WriteVCD(c.merge, c.binDir, tap); err != nil {
		tmp.Abort()
		return err
	}
	built := tap.h.Sum(nil)
	if err := tmp.Commit(); err != nil {
		return err
	}
	for _, m := range c.manifests {
		if err := e.disk.CopyToDest(ctx, filepath.Join(m.dir, m.name),
			bytes.NewReader([]byte(m.content)), int64(len(m.content)), nil); err != nil {
			// Manifests are identical across sibling jobs: drop only the
			// job-exclusive VCD, never shared manifests.
			e.disk.Remove(vcdPath)
			return err
		}
	}
	tr.setPhase(PhaseVerifying)
	ok, err := e.hashFile(vcdPath, built)
	if err != nil {
		e.disk.Remove(vcdPath)
		return err
	}
	if !ok {
		e.disk.Remove(vcdPath)
		return fmt.Errorf("verify %s: content mismatch", vcdPath)
	}
	if fi, err := e.disk.Stat(vcdPath); err != nil || fi.Size() != c.total {
		e.disk.Remove(vcdPath)
		return fmt.Errorf("verify %s: size mismatch", vcdPath)
	}
	return nil
}

// runSplit streams the ISO into root chunks (single writer goroutine owns
// the volume, so direct chunk writes keep the contiguity contract), then
// verifies index + sizes + content hash.
func (e *Executor) runSplit(ctx context.Context, w workItem, tr *tracker) error {
	s := w.split
	tr.setPhase(PhaseSplitting)
	src, err := os.Open(s.srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	srcHash := sha256.New()
	counted := &countReader{r: io.TeeReader(src, srcHash), tr: tr}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-tick.C:
				_ = e.store.Checkpoint(w.job.ID, PhaseSplitting, counted.nNow())
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	if _, err := usbextreme.Write(s.destDir, s.oplName, s.serial,
		cancelReader(ctx, counted), s.size); err != nil {
		return err
	}
	tr.setPhase(PhaseVerifying)
	if err := usbextreme.Verify(s.destDir, s.serial); err != nil {
		return fmt.Errorf("verify ul set: %w", err)
	}
	r, total, err := usbextreme.Open(s.destDir, s.serial)
	if err != nil {
		return err
	}
	defer r.Close()
	back, err := hashStream(r)
	if err != nil {
		return fmt.Errorf("verify ul readback: %w", err)
	}
	if total != s.size || !equalHash(back, srcHash.Sum(nil)) {
		return fmt.Errorf("verify ul set %s: content mismatch", s.serial)
	}
	return nil
}

// verifyFile hash-checks a copied file against the source hash + size.
func (e *Executor) verifyFile(tr *tracker, destPath string, size int64, want []byte) error {
	tr.setPhase(PhaseVerifying)
	ok, err := e.hashFile(destPath, want)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("verify %s: content mismatch", destPath)
	}
	if fi, err := e.disk.Stat(destPath); err != nil || fi.Size() != size {
		return fmt.Errorf("verify %s: size mismatch", destPath)
	}
	return nil
}

// countReader counts bytes through to the progress tracker (atomic: a
// checkpoint ticker reads n() concurrently).
type countReader struct {
	r  io.Reader
	tr *tracker
	n  atomic.Int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.n.Add(int64(n))
		c.tr.add(int64(n))
	}
	return n, err
}

func (c *countReader) nNow() int64 { return c.n.Load() }

// cancelReader aborts reads once ctx is done (usbextreme takes a plain
// reader, so cancellation lands between its internal chunk copies).
func cancelReader(ctx context.Context, r io.Reader) io.Reader {
	return &cancelAdapter{ctx: ctx, r: r}
}

type cancelAdapter struct {
	ctx context.Context
	r   io.Reader
}

func (c *cancelAdapter) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
	}
	return c.r.Read(p)
}

// hashFile streams a destination file's SHA-256 through Disk and compares.
func (e *Executor) hashFile(path string, want []byte) (bool, error) {
	f, err := e.disk.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	return hashCompare(f, want)
}

// hashStream hashes an open reader (ul chunk chains).
func hashStream(r io.Reader) ([]byte, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func hashCompare(r io.Reader, want []byte) (bool, error) {
	got, err := hashStream(r)
	if err != nil {
		return false, err
	}
	return equalHash(got, want), nil
}

func equalHash(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
