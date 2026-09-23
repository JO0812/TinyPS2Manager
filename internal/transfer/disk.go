package transfer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileDisk writes destination files against the real filesystem with the
// contiguity contract (spec §6.4 #5): bytes stream into a temp file on the
// SAME volume, fsync, then rename into place — one allocation extent, no
// cross-volume copy. On FAT32 the rename is a directory-entry update
// (fast, near-atomic); a power loss may orphan either name, which the
// executor's post-copy Verify catches. It structurally satisfies the
// queue.Disk interface (defined in M2-3); transfer never imports queue.
type FileDisk struct{}

// TempFile is a same-volume staging file: bytes stream in, Commit renames
// it to the bound final name, Abort deletes the partial. Temps always
// carry the .oplbm. infix so SweepStaleTemps can recognize them.
type TempFile interface {
	io.Writer
	Commit() error
	Abort()
}

// Disk abstracts every destination write. FileDisk is the production
// implementation; the queue executor takes this interface so tests use an
// in-memory fake.
type Disk interface {
	MkdirAll(path string) error
	// CopyToDest streams exactly size bytes to finalPath via same-volume
	// temp + rename, reporting cumulative bytes (nil-safe callback).
	CopyToDest(ctx context.Context, finalPath string, src io.Reader, size int64, onProgress func(int64)) error
	// StageFile creates a temp in dir bound to finalName.
	StageFile(dir, finalName string) (TempFile, error)
	Remove(path string) error
	Stat(path string) (os.FileInfo, error)
	// Open reads a destination file back (post-copy verification).
	Open(path string) (io.ReadCloser, error)
	// SweepStaleTemps deletes orphaned .oplbm.* temps under root
	// (crash recovery), returning the removal count.
	SweepStaleTemps(root string) (int, error)
}

// StagedFile is FileDisk's TempFile.
type StagedFile struct {
	f     *os.File
	tmp   string
	final string
	done  bool
}

// StageFile creates dir/.oplbm.* bound to dir/finalName.
func (FileDisk) StageFile(dir, finalName string) (TempFile, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".oplbm.*")
	if err != nil {
		return nil, err
	}
	return &StagedFile{f: f, tmp: f.Name(), final: filepath.Join(dir, finalName)}, nil
}

// Write streams into the temp.
func (s *StagedFile) Write(p []byte) (int, error) { return s.f.Write(p) }

// Commit syncs, closes, and renames into place.
func (s *StagedFile) Commit() error {
	if err := s.f.Sync(); err != nil {
		s.Abort()
		return fmt.Errorf("sync %s: %w", s.final, err)
	}
	if err := s.f.Close(); err != nil {
		os.Remove(s.tmp)
		return fmt.Errorf("close %s: %w", s.final, err)
	}
	if err := os.Rename(s.tmp, s.final); err != nil {
		os.Remove(s.tmp)
		return fmt.Errorf("rename %s: %w", s.final, err)
	}
	s.done = true
	return nil
}

// Abort closes and deletes the partial temp. Safe to call twice or after
// Commit (no-op once committed).
func (s *StagedFile) Abort() {
	if s.done {
		return
	}
	s.done = true
	s.f.Close()
	os.Remove(s.tmp)
}

// SweepStaleTemps deletes orphaned .oplbm.* temps under root (crash
// recovery), returning the removal count. Best-effort: the walk continues
// past individual failures; the first error is returned alongside the count.
func (FileDisk) SweepStaleTemps(root string) (int, error) {
	var count int
	var first error
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if first == nil {
				first = err
			}
			return nil
		}
		if d.IsDir() || !strings.Contains(d.Name(), ".oplbm.") {
			return nil
		}
		if rerr := os.Remove(path); rerr != nil {
			if first == nil {
				first = rerr
			}
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		return count, err
	}
	return count, first
}

// MkdirAll creates a directory tree.
func (FileDisk) MkdirAll(path string) error { return os.MkdirAll(path, 0o755) }

// Remove deletes a file or empty dir (partial-output cleanup).
func (FileDisk) Remove(path string) error { return os.Remove(path) }

// Stat stats a destination path.
func (FileDisk) Stat(path string) (os.FileInfo, error) { return os.Stat(path) }

// Open reads a destination file back.
func (FileDisk) Open(path string) (io.ReadCloser, error) { return os.Open(path) }

// CopyToDest streams exactly size bytes from src to finalPath via a
// same-volume temp file, reporting cumulative bytes through onProgress
// (nil-safe). Short or long sources are errors; temp files never leak.
func (FileDisk) CopyToDest(ctx context.Context, finalPath string, src io.Reader, size int64, onProgress func(int64)) error {
	if size < 0 {
		return fmt.Errorf("negative size %d", size)
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(finalPath), ".oplbm.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		if !done {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	var written int64
	src = ctxReader{ctx: ctx, r: src}
	for written < size {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := tmp.ReadFrom(io.LimitReader(src, size-written))
		written += n
		if onProgress != nil {
			onProgress(written)
		}
		if err != nil {
			return fmt.Errorf("write %s: %w", finalPath, err)
		}
		if n == 0 {
			break
		}
	}
	if written != size {
		return fmt.Errorf("%s: source gave %d of %d bytes", finalPath, written, size)
	}
	// A 1-byte over-read proves the source isn't longer than stated.
	if b := make([]byte, 1); readOne(src, b) {
		return fmt.Errorf("%s: source larger than stated size %d", finalPath, size)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", finalPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", finalPath, err)
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename %s: %w", finalPath, err)
	}
	done = true
	return nil
}

func readOne(r io.Reader, b []byte) bool {
	_, err := io.ReadFull(r, b)
	return err == nil
}

// ctxReader aborts reads once ctx is done (cancellation lands between 1 MiB
// chunks — prompt enough for multi-GB copies without per-byte overhead).
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
	}
	return c.r.Read(p)
}
