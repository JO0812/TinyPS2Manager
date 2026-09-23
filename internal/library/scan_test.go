package library

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildScanTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	iso := "fake-iso-bytes"
	writeFile(t, filepath.Join(root, "game.iso"), iso)
	writeFile(t, filepath.Join(root, "dup", "game-copy.iso"), iso) // deduped
	writeFile(t, filepath.Join(root, "ps2", "Saga (Disc 1).iso"), "saga-one!!")
	writeFile(t, filepath.Join(root, "ps2", "Saga (Disc 2).iso"), "saga-two!!")
	ps1 := filepath.Join(root, "ps1")
	writeFile(t, filepath.Join(ps1, "d1.bin"), "bin-one!!")
	writeFile(t, filepath.Join(ps1, "d2.bin"), "bin-two!!")
	writeFile(t, filepath.Join(ps1, "single.bin"), "bin-single")
	writeFile(t, filepath.Join(ps1, "Game (Disc 1).cue"), sprintfCue("d1.bin"))
	writeFile(t, filepath.Join(ps1, "Game (Disc 2).cue"), sprintfCue("d2.bin"))
	writeFile(t, filepath.Join(ps1, "Single.cue"), sprintfCue("single.bin"))
	writeFile(t, filepath.Join(ps1, "Broken.cue"), sprintfCue("missing.bin"))
	writeFile(t, filepath.Join(ps1, "Garbage.cue"), "this is not a cue sheet\n")
	writeFile(t, filepath.Join(ps1, "lone.bin"), "orphan")
	return root
}

func sprintfCue(bin string) string {
	return "FILE \"" + bin + "\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"
}

func TestScanDir(t *testing.T) {
	items, err := ScanDir(buildScanTree(t))
	if err != nil {
		t.Fatalf("ScanDir: %v", err)
	}
	byPath := map[string]LibraryItem{}
	for _, it := range items {
		byPath[filepath.Base(it.SourcePath)] = it
	}
	if len(items) != 8 {
		t.Fatalf("got %d items: %v", len(items), items)
	}
	iso, ok := byPath["game.iso"]
	if !ok || iso.Platform != PlatformPS2 || iso.Title != "game" {
		t.Errorf("iso item = %+v", iso)
	}
	if _, dup := byPath["game-copy.iso"]; dup {
		t.Error("duplicate content scanned twice")
	}
	d1, d2 := byPath["Game (Disc 1).cue"], byPath["Game (Disc 2).cue"]
	for _, d := range []LibraryItem{d1, d2} {
		if d.Platform != PlatformPS1 || d.Title != "Game" || d.DiscType != "" {
			t.Errorf("disc item = %+v", d)
		}
	}
	if d1.DiscGroupID == nil || d2.DiscGroupID == nil ||
		*d1.DiscGroupID != *d2.DiscGroupID {
		t.Errorf("discs not grouped: %v %v", d1.DiscGroupID, d2.DiscGroupID)
	}
	if d1.DiscIndex != 1 || d2.DiscIndex != 2 {
		t.Errorf("indices %d,%d, want 1,2", d1.DiscIndex, d2.DiscIndex)
	}
	s1, s2 := byPath["Saga (Disc 1).iso"], byPath["Saga (Disc 2).iso"]
	if s1.Platform != PlatformPS2 || s1.Title != "Saga" {
		t.Errorf("ps2 disc item = %+v", s1)
	}
	if s1.DiscGroupID == nil || s2.DiscGroupID == nil ||
		*s1.DiscGroupID != *s2.DiscGroupID {
		t.Errorf("ps2 discs not grouped: %v %v", s1.DiscGroupID, s2.DiscGroupID)
	}
	if s1.DiscIndex != 1 || s2.DiscIndex != 2 {
		t.Errorf("ps2 indices %d,%d, want 1,2", s1.DiscIndex, s2.DiscIndex)
	}
	single := byPath["Single.cue"]
	if single.DiscGroupID != nil || single.SizeBytes != int64(len("bin-single")) {
		t.Errorf("single = %+v", single)
	}
	if broken := byPath["Broken.cue"]; broken.Status != StatusError {
		t.Errorf("broken cue status = %q", broken.Status)
	}
	if garbage := byPath["Garbage.cue"]; garbage.Status != StatusError {
		t.Errorf("garbage cue status = %q", garbage.Status)
	}
}

func TestScanDirMissingRoot(t *testing.T) {
	if _, err := ScanDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected error for missing root")
	}
}

func TestSplitDiscSuffix(t *testing.T) {
	cases := map[string]struct {
		title string
		index int
	}{
		"Game (Disc 1)":  {"Game", 1},
		"Game (DISK 02)": {"Game", 2},
		"Game - disc 3":  {"Game", 3},
		"Game_CD4":       {"Game", 4},
		"CD Player":      {"CD Player", 0},
		"Game":           {"Game", 0},
		"(Disc 1)":       {"(Disc 1)", 0},
	}
	for in, want := range cases {
		title, index := splitDiscSuffix(in)
		if title != want.title || index != want.index {
			t.Errorf("%q -> (%q,%d), want (%q,%d)", in, title, index, want.title, want.index)
		}
	}
}
