package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "game.iso"), []byte("fake-iso-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	ps1 := filepath.Join(root, "ps1")
	if err := os.MkdirAll(ps1, 0o755); err != nil {
		t.Fatal(err)
	}
	cue := func(bin string) string {
		return "FILE \"" + bin + "\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"
	}
	if err := os.WriteFile(filepath.Join(ps1, "d1.bin"), []byte("bin-one!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ps1, "d2.bin"), []byte("bin-two!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ps1, "Epic (Disc 1).cue"), []byte(cue("d1.bin")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ps1, "Epic (Disc 2).cue"), []byte(cue("d2.bin")), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := LoadBundled()
	if err != nil {
		t.Fatal(err)
	}
	items, err := ImportDir(st, root, db)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	// The pair shares one real (>= 1) group ID.
	var groups []int64
	for _, it := range items {
		if it.Title == "Epic" {
			if it.DiscGroupID == nil {
				t.Fatalf("Epic disc ungrouped: %+v", it)
			}
			groups = append(groups, *it.DiscGroupID)
		}
		if it.Platform == PlatformPS2 && (it.DiscType == "" || it.DetectionMethod == "") {
			t.Errorf("iso undetected: %+v", it)
		}
	}
	if len(groups) != 2 || groups[0] != groups[1] || groups[0] < 1 {
		t.Errorf("groups = %v", groups)
	}
	// Persisted state matches the return value.
	list, err := st.List()
	if err != nil || len(list) != 3 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
	// Re-import keeps IDs (dedupe) and detection.
	again, err := ImportDir(st, root, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 3 || again[0].ID != items[0].ID {
		t.Errorf("re-import duplicated rows: %+v", again)
	}
}
