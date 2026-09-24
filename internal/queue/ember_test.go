package queue

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

func writeBios(t *testing.T, destPath, prefix string, size int64) {
	t.Helper()
	dir := filepath.Join(destPath, prefix, "EMBER")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bios := filepath.Join(dir, "bios.bin")
	data := make([]byte, size)
	if err := os.WriteFile(bios, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func makePS1Cue(t *testing.T, dir string) string {
	t.Helper()
	bin := make([]byte, 4*2352)
	for i := range bin {
		bin[i] = byte(i % 256)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	bin2 := make([]byte, 2*2352)
	if err := os.WriteFile(filepath.Join(dir, "game2.bin"), bin2, 0o644); err != nil {
		t.Fatal(err)
	}
	cue := "FILE \"game.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\nFILE \"game2.bin\" BINARY\n  TRACK 02 MODE2/2352\n    INDEX 01 00:00:00\n"
	cuePath := filepath.Join(dir, "game.cue")
	if err := os.WriteFile(cuePath, []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	return cuePath
}

func TestEmberBiosGate(t *testing.T) {
	qs, _ := Open(":memory:")
	defer qs.Close()
	ls, _ := library.Open(":memory:")
	defer ls.Close()
	dest, _ := qs.AddDestination(Destination{Path: t.TempDir(), Kind: DestFolder})

	srcDir := t.TempDir()
	cue := makePS1Cue(t, srcDir)
	it, _ := ls.UpsertItem(library.LibraryItem{SourcePath: cue, ContentHash: "h-ember", Platform: library.PlatformPS1, Title: "Ember Game", SizeBytes: 100})

	// Missing BIOS
	if _, _, err := EstimateEmber(&library.LibraryItem{ID: it.ID, SourcePath: cue, Platform: library.PlatformPS1, Title: "Ember Game"}, &dest); err == nil || !strings.Contains(err.Error(), "place your own `EMBER/bios.bin`") {
		t.Fatalf("missing bios: expected exact message, got %v", err)
	}
	// Wrong size
	writeBios(t, dest.Path, dest.BDMPrefix, 100)
	if _, _, err := EstimateEmber(&library.LibraryItem{SourcePath: cue, Platform: library.PlatformPS1, Title: "T"}, &dest); err == nil || !strings.Contains(err.Error(), "512 KB") {
		t.Fatalf("wrong size: %v", err)
	}
	// Correct
	os.Remove(filepath.Join(dest.Path, "EMBER", "bios.bin"))
	writeBios(t, dest.Path, dest.BDMPrefix, 512*1024)
	if _, total, err := EstimateEmber(&library.LibraryItem{SourcePath: cue, Platform: library.PlatformPS1, Title: "T"}, &dest); err != nil {
		t.Fatalf("correct bios: %v", err)
	} else if total == 0 {
		t.Error("total 0")
	}
}

func TestEmberPlanAndExecute(t *testing.T) {
	qs, _ := Open(":memory:")
	defer qs.Close()
	ls, _ := library.Open(":memory:")
	defer ls.Close()
	dest, _ := qs.AddDestination(Destination{Path: t.TempDir(), Kind: DestFolder, Filesystem: "exfat"})
	writeBios(t, dest.Path, dest.BDMPrefix, 512*1024)

	srcDir := t.TempDir()
	cue := makePS1Cue(t, srcDir)
	it, _ := ls.UpsertItem(library.LibraryItem{SourcePath: cue, ContentHash: "h-ember2", Platform: library.PlatformPS1, Title: "My Game", SizeBytes: 123})
	// Preview
	pv, err := PreviewEmber(&library.LibraryItem{SourcePath: cue, Platform: library.PlatformPS1, Title: "My Game"}, &dest)
	if err != nil {
		t.Fatalf("PreviewEmber: %v", err)
	}
	if len(pv.Files) != 3 { // cue + 2 bins
		t.Fatalf("files = %v", pv.Files)
	}
	if pv.DestDir != filepath.Join(dest.Path, "EMBER", "games", "My Game") {
		t.Errorf("destDir = %q", pv.DestDir)
	}

	// Enqueue and execute
	jobs, err := qs.Enqueue([]Job{{LibraryItemID: it.ID, DestinationID: dest.ID, Kind: KindEmberCopy, BytesTotal: pv.Total}})
	if err != nil {
		t.Fatal(err)
	}
	ex := New(qs, ls, transfer.FileDisk{}, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ex.Run(ctx)
	// Poll
	deadline := time.Now().Add(5 * time.Second)
	for {
		j, _ := qs.GetJob(jobs[0].ID)
		if j.Status == JobDone {
			break
		}
		if j.Status == JobError {
			t.Fatalf("job error: %s", j.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %+v", j)
		}
		time.Sleep(50 * time.Millisecond)
	}
	j, _ := qs.GetJob(jobs[0].ID)
	if j.Status != JobDone {
		t.Fatalf("job not done: %+v", j)
	}
	// Verify files copied preserving names
	if _, err := os.Stat(filepath.Join(dest.Path, "EMBER", "games", "My Game", "game.cue")); err != nil {
		t.Errorf("cue not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest.Path, "EMBER", "games", "My Game", "game.bin")); err != nil {
		t.Errorf("bin not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest.Path, "EMBER", "games", "My Game", "game2.bin")); err != nil {
		t.Errorf("bin2 not copied: %v", err)
	}
	// Verify CUE content preserved
	orig, _ := os.ReadFile(cue)
	copied, _ := os.ReadFile(filepath.Join(dest.Path, "EMBER", "games", "My Game", "game.cue"))
	if string(orig) != string(copied) {
		t.Error("CUE content not preserved byte-for-byte")
	}
}
