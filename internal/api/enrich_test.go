package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/jo/TinyPS2Manager/internal/cheats"
	"github.com/jo/TinyPS2Manager/internal/library"
)

func cheatItem() library.LibraryItem {
	return library.LibraryItem{ID: 1, Title: "Game (USA)", Platform: library.PlatformPS2, GameID: "SLUS_213.85"}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const handCHT = "Hand\n90111111 11111111\nHP\n20111111 00000001\n"

// Auto order is hand > widescreen > database; forced sources never fall back.
func TestPickCheatContent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hand", "SLUS_213.85.cht"), handCHT)
	writeFile(t, filepath.Join(dir, "wide", "SLUS_213.85.cht"), "Wide\n90222222 22222222\nC\n20222222 00000002\n")
	dbMap := map[string]cheats.RawGame{
		"Game (USA)": {Title: "Game (USA)", Cheats: []cheats.RawCheat{
			{Name: "Master", Codes: []string{"90333333 33333333"}},
			{Name: "Ammo", Codes: []string{"20333333 00000063"}},
		}},
	}
	wideMap, err := cheats.ParseWidescreenDir(filepath.Join(dir, "wide"))
	if err != nil {
		t.Fatal(err)
	}
	it := cheatItem()

	content, winner, _, err := pickCheatContent(it, "auto", wideMap, dbMap, filepath.Join(dir, "hand"))
	if err != nil || winner != "hand" || content != handCHT {
		t.Errorf("auto = %q,%q,%v; want hand winner", winner, content, err)
	}
	if _, winner, _, err := pickCheatContent(it, "auto", wideMap, dbMap, ""); err != nil || winner != "widescreen" {
		t.Errorf("auto-no-hand = %q,%v; want widescreen", winner, err)
	}
	if c, winner, _, err := pickCheatContent(it, "auto", nil, dbMap, ""); err != nil || winner != "database" || c == "" {
		t.Errorf("auto-db-only = %q,%v; want database", winner, err)
	}
	if _, _, _, err := pickCheatContent(it, "widescreen", nil, dbMap, ""); err == nil {
		t.Error("forced widescreen with no entry: expected missing error, no fallback")
	}
	if _, _, _, err := pickCheatContent(it, "bogus", wideMap, dbMap, ""); err == nil {
		t.Error("unknown source: expected error")
	}
	if _, _, _, err := pickCheatContent(it, "hand", wideMap, dbMap, ""); err == nil {
		t.Error("forced hand with no dir: expected error")
	}
	// Region verdict travels with the item, not the source.
	if !cheats.RegionMatches(it.GameID, it.Title) {
		t.Error("US serial + (USA) title should match")
	}
	if cheats.RegionMatches("SLES_512.30", "Game (USA)") {
		t.Error("EU serial + (USA) title should mismatch")
	}
}

func TestEnrichKindRejected(t *testing.T) {
	h, cancel := newAPIHarness(t)
	defer cancel()
	code, raw := h.do("POST", "/api/queue", map[string]any{
		"destinationPath": t.TempDir(), "itemIds": []int64{}, "kind": "enrich",
	})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("enrich enqueue = %d, want 422\n%s", code, raw)
	}
}
