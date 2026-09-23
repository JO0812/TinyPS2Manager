package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMapFilesystem(t *testing.T) {
	cases := map[string]Filesystem{
		"vfat": FSFAT32, "FAT32": FSFAT32, "msdos": FSFAT32, "fat": FSFAT32,
		"exfat": FSExFAT, "exFAT": FSExFAT,
		"ext4": FSUnknown, "overlay": FSUnknown, "tmpfs": FSUnknown, "": FSUnknown,
		"apfs": FSUnknown, "ntfs": FSUnknown,
	}
	for raw, want := range cases {
		if got := mapFilesystem(raw); got != want {
			t.Errorf("%q -> %q, want %q", raw, got, want)
		}
	}
}

func TestProbeTempDir(t *testing.T) {
	got, err := Probe(t.TempDir())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got.FreeBytes <= 0 {
		t.Errorf("free bytes = %d", got.FreeBytes)
	}
	t.Logf("temp dir: fs=%q free=%d", got.Filesystem, got.FreeBytes)
}

func randomReader(t *testing.T, n int64) io.Reader {
	t.Helper()
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

func TestCopyToDestRoundTrip(t *testing.T) {
	d := FileDisk{}
	dir := t.TempDir()
	const size = 3 << 20
	src := randomReader(t, size)
	var progress []int64
	final := filepath.Join(dir, "sub", "game.iso")
	if err := d.CopyToDest(context.Background(), final, src, size,
		func(done int64) { progress = append(progress, done) }); err != nil {
		t.Fatalf("CopyToDest: %v", err)
	}
	fi, err := os.Stat(final)
	if err != nil || fi.Size() != size {
		t.Fatalf("stat = %v, %v", fi, err)
	}
	if len(progress) == 0 || progress[len(progress)-1] != size {
		t.Errorf("progress never reached %d: last %v", size, progress[len(progress)-1:])
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Errorf("progress regressed: %v", progress)
			break
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "sub", ".oplbm.*"))
	if len(leftovers) != 0 {
		t.Errorf("temp leftovers: %v", leftovers)
	}
}

func TestCopyToDestSizeMismatch(t *testing.T) {
	d := FileDisk{}
	for _, tc := range []struct {
		name string
		src  int64
		size int64
	}{
		{"short", 100, 200},
		{"long", 300, 200},
	} {
		dir := t.TempDir()
		final := filepath.Join(dir, "f.iso")
		err := d.CopyToDest(context.Background(), final, randomReader(t, tc.src), tc.size, nil)
		if err == nil {
			t.Errorf("%s: expected error", tc.name)
			continue
		}
		if _, serr := os.Stat(final); !os.IsNotExist(serr) {
			t.Errorf("%s: final file survives failure", tc.name)
		}
		leftovers, _ := filepath.Glob(filepath.Join(dir, ".oplbm.*"))
		if len(leftovers) != 0 {
			t.Errorf("%s: temp survives failure: %v", tc.name, leftovers)
		}
	}
}

func TestCopyToDestCancel(t *testing.T) {
	d := FileDisk{}
	dir := t.TempDir()
	final := filepath.Join(dir, "f.iso")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := d.CopyToDest(ctx, final, randomReader(t, 3<<20), 3<<20, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if _, serr := os.Stat(final); !os.IsNotExist(serr) {
		t.Error("final file survives cancel")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".oplbm.*"))
	if len(leftovers) != 0 {
		t.Errorf("temp survives cancel: %v", leftovers)
	}
}

func TestCopyToDestZeroBytes(t *testing.T) {
	d := FileDisk{}
	final := filepath.Join(t.TempDir(), "empty.iso")
	if err := d.CopyToDest(context.Background(), final,
		bytes.NewReader(nil), 0, nil); err != nil {
		t.Fatalf("zero-byte copy: %v", err)
	}
	fi, err := os.Stat(final)
	if err != nil || fi.Size() != 0 {
		t.Errorf("stat = %v, %v", fi, err)
	}
}

func TestMkdirRemoveStat(t *testing.T) {
	d := FileDisk{}
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := d.MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Stat(p); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove(p); !os.IsNotExist(err) {
		t.Errorf("double remove = %v", err)
	}
}

func TestCtxReaderStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := ctxReader{ctx: ctx, r: strings.NewReader("data")}
	if _, err := r.Read(make([]byte, 4)); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}
