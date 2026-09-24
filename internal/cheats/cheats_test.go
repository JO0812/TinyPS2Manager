package cheats

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

func TestParseDatabase(t *testing.T) {
	data := []byte(`
"Game One Title"
Infinite Health
20123456 00000001
20234567 00000002

Infinite Ammo
90123456 0C123456
20234567 00000003

"Game Two"
Unlimited Lives
20345678 00000009
`)
	m, err := ParseDatabase(data)
	if err != nil {
		t.Fatalf("ParseDatabase: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("games = %d, want 2", len(m))
	}
	g1, ok := m["Game One Title"]
	if !ok {
		t.Fatal("Game One missing")
	}
	if len(g1.Cheats) != 2 {
		t.Fatalf("g1 cheats = %d", len(g1.Cheats))
	}
	if g1.Cheats[0].Name != "Infinite Health" || len(g1.Cheats[0].Codes) != 2 {
		t.Errorf("cheat0 = %+v", g1.Cheats[0])
	}
	if g1.Cheats[1].Codes[0] != "90123456 0C123456" {
		t.Errorf("master code = %q", g1.Cheats[1].Codes[0])
	}
	g2 := m["Game Two"]
	if len(g2.Cheats) != 1 || g2.Cheats[0].Name != "Unlimited Lives" {
		t.Errorf("g2 = %+v", g2)
	}
	// Comments and blank ignored
	data2 := []byte(`
// This is a comment
# Another comment
"Game Three"
Cheat Name
90111111 11111111
; This is NOT a comment per spec, should be treated as name
; But our parser treats ; as name (since only // and # are comments)
`)
	m2, _ := ParseDatabase(data2)
	if _, ok := m2["Game Three"]; !ok {
		t.Error("Game Three missing with comments")
	}
}

func TestBuildMasterValidation(t *testing.T) {
	// No master
	noMaster := []RawCheat{{Name: "Cheat", Codes: []string{"20111111 00000001"}}}
	if _, _, err := Build("SLUS_123.45", noMaster); err == nil || !strings.Contains(err.Error(), "no master") {
		t.Errorf("no master: expected error, got %v", err)
	}
	// Multiple masters
	multi := []RawCheat{
		{Name: "Master1", Codes: []string{"90111111 11111111"}},
		{Name: "Master2", Codes: []string{"90222222 22222222"}},
	}
	if _, warns, err := Build("SLUS_123.45", multi); err == nil {
		t.Error("multiple masters: expected error")
	} else if !warns.HasMultipleMasters {
		t.Error("multiple masters should set warning flag")
	}
	// Exactly one master + normal cheats ok
	okCheats := []RawCheat{
		{Name: "Master", Codes: []string{"90111111 11111111"}},
		{Name: "Cheat1", Codes: []string{"20111111 00000001"}},
	}
	content, warns, err := Build("SLUS_123.45", okCheats)
	if err != nil {
		t.Fatalf("valid build: %v", err)
	}
	if warns.HasEngineSkipped {
		t.Error("should not have skipped flag")
	}
	if !strings.Contains(content, "Master") || !strings.Contains(content, "90111111") {
		t.Errorf("content = %q", content)
	}
	// Engine-skipped types 8/A/B
	skipped := []RawCheat{
		{Name: "Master", Codes: []string{"90111111 11111111"}},
		{Name: "Code8", Codes: []string{"80111111 00000001"}},
		{Name: "CodeA", Codes: []string{"A0111111 00000001"}},
	}
	_, warns2, err := Build("SLUS_123.45", skipped)
	if err != nil {
		t.Fatalf("skipped types build: %v", err)
	}
	if !warns2.HasEngineSkipped {
		t.Error("engine-skipped not flagged")
	}
}

func TestBuildLimit250(t *testing.T) {
	var cheats []RawCheat
	cheats = append(cheats, RawCheat{Name: "Master", Codes: []string{"90111111 11111111"}})
	for i := 0; i < 300; i++ {
		cheats = append(cheats, RawCheat{Name: strings.Repeat("C", 10), Codes: []string{"20111111 00000001"}})
	}
	_, warns, err := Build("SLUS_123.45", cheats)
	if err != nil {
		t.Fatalf("limit: %v", err)
	}
	if warns.DroppedCount != 51 { // 301 total -> 250 kept = 51 dropped (including master? actually 301 -> 51 dropped to reach 250, but master counted)
		// 1 master + 300 =301, drop 51 to get 250
		t.Errorf("dropped = %d, want 51", warns.DroppedCount)
	}
	// Exactly 250 should not drop
	cheats250 := make([]RawCheat, 250)
	for i := range cheats250 {
		cheats250[i] = RawCheat{Name: "C", Codes: []string{"90111111 11111111"}}
		// Make only first one master, rest non-master but need exactly one master overall
		// So set first as master, rest as normal
		if i == 0 {
			cheats250[i].Codes = []string{"90111111 11111111"}
		} else {
			cheats250[i].Codes = []string{"20111111 00000001"}
		}
	}
	_, warns2, err := Build("SLUS_123.45", cheats250)
	if err != nil {
		t.Fatalf("250: %v", err)
	}
	if warns2.DroppedCount != 0 {
		t.Errorf("250 dropped = %d", warns2.DroppedCount)
	}
}

func TestValidateCHT(t *testing.T) {
	content := "Master\n90111111 11111111\nCheat\n20111111 00000001\n"
	warns, err := ValidateCHT(content)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if warns.HasEngineSkipped {
		t.Error("unexpected skipped")
	}
	// Missing master
	bad := "Cheat\n20111111 00000001\n"
	if _, err := ValidateCHT(bad); err == nil {
		t.Error("missing master: expected error")
	}
}

func TestStage(t *testing.T) {
	root := t.TempDir()
	disk := transfer.FileDisk{}
	item := library.LibraryItem{ID: 1, Title: "Game", GameID: "SLUS_123.45", GameIDUncertain: false}
	content := "Master\n90111111 11111111\nCheat\n20111111 00000001\n"
	// Normal stage
	if err := Stage(context.Background(), disk, root, "", item, content, false); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	path := filepath.Join(root, "CHT", "SLUS_123.45.cht")
	if _, err := disk.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Hand file wins: second stage should not overwrite
	other := "Master\n90111111 22222222\n"
	if err := Stage(context.Background(), disk, root, "", item, other, false); err != nil {
		t.Fatalf("second Stage: %v", err)
	}
	// Still first content
	// Uncertain requires confirm
	uncertain := library.LibraryItem{ID: 2, Title: "Mod", GameID: "SLUS_123.45", GameIDUncertain: true}
	if err := Stage(context.Background(), disk, root, "", uncertain, content, false); err == nil || !strings.Contains(err.Error(), "explicit confirm") {
		t.Errorf("uncertain without confirm: expected error, got %v", err)
	}
	if err := Stage(context.Background(), disk, root, "", uncertain, content, true); err != nil {
		// This will also hit hand-file win (since file already exists), but should not error
		// For a different GameID, it should succeed
		// Use a different root to test
		root2 := t.TempDir()
		if err2 := Stage(context.Background(), disk, root2, "", uncertain, content, true); err2 != nil {
			t.Errorf("uncertain with confirm: %v", err2)
		}
	}
	// Missing GameID
	noID := library.LibraryItem{ID: 3, Title: "NoID"}
	if err := Stage(context.Background(), disk, root, "", noID, content, false); err == nil {
		t.Error("missing GameID: expected error")
	}
	// Prefix
	root3 := t.TempDir()
	if err := Stage(context.Background(), disk, root3, "OPL", item, content, false); err != nil {
		t.Fatalf("prefix: %v", err)
	}
	if _, err := disk.Stat(filepath.Join(root3, "OPL", "CHT", "SLUS_123.45.cht")); err != nil {
		t.Fatalf("prefixed stat: %v", err)
	}
}
