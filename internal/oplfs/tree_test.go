package oplfs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTreePathsBasic(t *testing.T) {
	p := &TreePlan{Root: "/mnt/ps2"}
	p.Add(BucketDVD, "", "Game.iso")
	p.Add(BucketCD, "", "Blue.iso")
	p.Add(BucketPOPS, "", "Game.VCD")
	p.Add("", "", "ul.cfg")
	p.Add("", "", "ul.CRC.SLUS_111.11.00")
	dirs, files, err := p.Paths()
	if err != nil {
		t.Fatalf("Paths: %v", err)
	}
	for _, want := range []string{"/mnt/ps2/DVD", "/mnt/ps2/CD", "/mnt/ps2/POPS"} {
		if !contains(dirs, want) {
			t.Errorf("dirs lacks %s: %v", want, dirs)
		}
	}
	for _, want := range []string{
		"/mnt/ps2/DVD/Game.iso",
		"/mnt/ps2/CD/Blue.iso",
		"/mnt/ps2/POPS/Game.VCD",
		"/mnt/ps2/ul.cfg",
		"/mnt/ps2/ul.CRC.SLUS_111.11.00",
	} {
		if !contains(files, want) {
			t.Errorf("files lacks %s: %v", want, files)
		}
	}
}

func TestTreePathsPrefix(t *testing.T) {
	p := &TreePlan{Root: "/mnt/ps2", BDMprefix: "OPL"}
	p.Add(BucketDVD, "", "Game.iso")
	dirs, files, err := p.Paths()
	if err != nil {
		t.Fatalf("Paths: %v", err)
	}
	if !contains(dirs, filepath.Join("/mnt/ps2/OPL", "DVD")) {
		t.Errorf("dirs = %v", dirs)
	}
	if !contains(files, filepath.Join("/mnt/ps2/OPL/DVD", "Game.iso")) {
		t.Errorf("files = %v", files)
	}
}

func TestTreePathsErrors(t *testing.T) {
	cases := map[string]func(*TreePlan){
		"lowercase bucket": func(p *TreePlan) { p.Add("dvd", "", "G.iso") },
		"pops without vcd": func(p *TreePlan) { p.Add(BucketPOPS, "", "note.txt") },
		"root game image":  func(p *TreePlan) { p.Add("", "", "Game.iso") },
		"root with subdir": func(p *TreePlan) { p.Add("", "sub", "ul.cfg") },
		"separator in name": func(p *TreePlan) { p.Add(BucketDVD, "", "a/b.iso") },
		"empty name":       func(p *TreePlan) { p.Add(BucketDVD, "", "") },
		"nested subdir":    func(p *TreePlan) { p.Add(BucketDVD, "a/b", "G.iso") },
	}
	for name, fill := range cases {
		p := &TreePlan{Root: "/mnt/ps2"}
		fill(p)
		if _, _, err := p.Paths(); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
	bad := &TreePlan{Root: "/mnt/ps2", BDMprefix: "/abs"}
	bad.Add(BucketDVD, "", "G.iso")
	if _, _, err := bad.Paths(); err == nil {
		t.Error("absolute prefix: expected error")
	}
	dotdot := &TreePlan{Root: "/mnt/ps2", BDMprefix: "../evil"}
	dotdot.Add(BucketDVD, "", "G.iso")
	if _, _, err := dotdot.Paths(); err == nil {
		t.Error("dotdot prefix: expected error")
	}
}

func TestTreePathsExtraDirs(t *testing.T) {
	p := &TreePlan{Root: "/mnt/ps2", ExtraDirs: []string{"POPS/SharedVMC", "POPS"}}
	p.Add(BucketDVD, "", "Game.iso")
	dirs, _, err := p.Paths()
	if err != nil {
		t.Fatalf("Paths: %v", err)
	}
	if !contains(dirs, "/mnt/ps2/POPS/SharedVMC") {
		t.Errorf("dirs = %v", dirs)
	}
	// Deduplicated against implied dirs.
	count := 0
	for _, d := range dirs {
		if d == "/mnt/ps2/POPS" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("POPS appears %d times in %v", count, dirs)
	}
	bad := &TreePlan{Root: "/mnt/ps2", ExtraDirs: []string{"../evil"}}
	bad.Add(BucketDVD, "", "Game.iso")
	if _, _, err := bad.Paths(); err == nil {
		t.Error("evil extra dir: expected error")
	}
}

func TestDiscFolder(t *testing.T) {
	got, err := DiscFolder("Game (Disc 1).VCD")
	if err != nil || got != "Game (Disc 1)" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := DiscFolder("Game.iso"); err == nil {
		t.Error("expected error for non-VCD")
	}
}

func TestBuildDISCSTXT(t *testing.T) {
	got, err := BuildDISCSTXT([]string{"A.VCD", "B.VCD"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got != "A.VCD\nB.VCD\n" {
		t.Errorf("got %q", got)
	}
	for _, vcds := range [][]string{
		{"A.VCD"}, // single needs no manifest
		{"A.VCD", "B.VCD", "C.VCD", "D.VCD", "E.VCD"},
		{"A.VCD", "B.iso"},
		{"A.VCD", "A.VCD"},
		{strings.Repeat("A", 70) + ".VCD", "B.VCD"},
	} {
		if _, err := BuildDISCSTXT(vcds); err == nil {
			t.Errorf("%v: expected error, got nil", vcds)
		}
	}
	// 73-char boundary: 69 + ".VCD" is exactly 73 and must pass.
	ok := strings.Repeat("A", 69) + ".VCD"
	if _, err := BuildDISCSTXT([]string{ok, "B.VCD"}); err != nil {
		t.Errorf("73-char name rejected: %v", err)
	}
}

func TestBuildVMCDIRTXT(t *testing.T) {
	got, err := BuildVMCDIRTXT("Game (Disc 1)")
	if err != nil || got != "Game (Disc 1)\n" {
		t.Errorf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "a/b", `a\b`, "a:b", strings.Repeat("A", 104)} {
		if _, err := BuildVMCDIRTXT(bad); err == nil {
			t.Errorf("%q: expected error, got nil", bad)
		}
	}
	if _, err := BuildVMCDIRTXT(strings.Repeat("A", 103)); err != nil {
		t.Errorf("103-byte name rejected: %v", err)
	}
}

func TestMultiDiscArtifacts(t *testing.T) {
	s := MultiDiscSet{
		VCDs:   []string{"SCUS_945.67.Final Fantasy VII.VCD", "SCUS_946.67.Final Fantasy VII.VCD"},
		VMCDir: "SCUS_945.67.Final Fantasy VII",
	}
	files, dirs, err := s.Artifacts()
	if err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	if len(files) != 4 || len(dirs) != 2 {
		t.Fatalf("files=%d dirs=%d, want 4+2 (VMC dir == disc 1 folder, deduped)", len(files), len(dirs))
	}
	// Every disc folder gets both manifests.
	seen := map[string]int{}
	for _, f := range files {
		if f.Bucket != BucketPOPS {
			t.Errorf("file not in POPS: %+v", f)
		}
		seen[f.Subdir+"/"+f.Name]++
	}
	for _, want := range []string{
		"SCUS_945.67.Final Fantasy VII/DISCS.TXT",
		"SCUS_945.67.Final Fantasy VII/VMCDIR.TXT",
		"SCUS_946.67.Final Fantasy VII/DISCS.TXT",
		"SCUS_946.67.Final Fantasy VII/VMCDIR.TXT",
	} {
		if seen[want] != 1 {
			t.Errorf("missing %s in %v", want, seen)
		}
	}
	// Shared VMC dir created once.
	vmc := filepath.Join("POPS", "SCUS_945.67.Final Fantasy VII")
	count := 0
	for _, d := range dirs {
		if d == vmc {
			count++
		}
	}
	if count != 1 {
		t.Errorf("shared VMC dir appears %d times in %v", count, dirs)
	}
	// Artifacts feed straight back into a TreePlan.
	p := &TreePlan{Root: "/mnt/ps2", Files: files}
	p.Add(BucketPOPS, "", "SCUS_945.67.Final Fantasy VII.VCD")
	p.Add(BucketPOPS, "", "SCUS_946.67.Final Fantasy VII.VCD")
	if _, _, err := p.Paths(); err != nil {
		t.Errorf("artifacts rejected by tree: %v", err)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
