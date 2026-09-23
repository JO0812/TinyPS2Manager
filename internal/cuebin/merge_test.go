package cuebin

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSectors builds n deterministic 2352-byte sectors; sector i is filled
// with byte(i+1) so data is distinguishable from gap zeros.
func makeSectors(n int) []byte {
	out := make([]byte, 0, n*SectorSizeVCD)
	for i := 0; i < n; i++ {
		for j := 0; j < SectorSizeVCD; j++ {
			out = append(out, byte(i+1))
		}
	}
	return out
}

const twoTrackSheet = `FILE "g.bin" BINARY
  TRACK 01 MODE2/2352
    INDEX 01 00:00:00
  TRACK 02 AUDIO
    INDEX 00 00:00:04
    INDEX 01 00:00:05
`

func TestBuildPlanTwoTrack(t *testing.T) {
	s, err := Parse(twoTrackSheet)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, err := BuildPlan(s, map[string]int64{"g.bin": 6 * SectorSizeVCD})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	want := []Op{
		{Kind: OpCopy, File: "g.bin", Offset: 0, Length: 4 * SectorSizeVCD},
		{Kind: OpZero, Sectors: 1},
		{Kind: OpCopy, File: "g.bin", Offset: 5 * SectorSizeVCD, Length: SectorSizeVCD},
	}
	if len(p.Ops) != len(want) {
		t.Fatalf("ops = %+v, want %+v", p.Ops, want)
	}
	for i := range want {
		if p.Ops[i] != want[i] {
			t.Errorf("op %d = %+v, want %+v", i, p.Ops[i], want[i])
		}
	}
	if p.TotalBytes != 6*SectorSizeVCD {
		t.Errorf("total = %d, want %d", p.TotalBytes, 6*SectorSizeVCD)
	}
}

func TestBuildPlanErrors(t *testing.T) {
	cases := map[string]struct {
		sheet string
		sizes map[string]int64
		want  string
	}{
		"non-binary": {
			"FILE \"g.bin\" MOTOROLA\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n",
			map[string]int64{"g.bin": SectorSizeVCD},
			"not BINARY",
		},
		"2048 mode": {
			"FILE \"g.bin\" BINARY\n  TRACK 01 MODE1/2048\n    INDEX 01 00:00:00\n",
			map[string]int64{"g.bin": SectorSizeVCD},
			"cannot merge verbatim",
		},
		"missing index 01": {
			"FILE \"g.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 00 00:00:00\n",
			map[string]int64{"g.bin": SectorSizeVCD},
			"missing INDEX 01",
		},
		"inverted pregap": {
			"FILE \"g.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n    INDEX 00 00:02:00\n",
			map[string]int64{"g.bin": 2 * SectorSizeVCD},
			"INDEX 00 follows INDEX 01",
		},
		"ragged bin": {
			"FILE \"g.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n",
			map[string]int64{"g.bin": 100},
			"not a multiple",
		},
		"unknown bin": {
			"FILE \"g.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 01 00:00:00\n",
			map[string]int64{},
			"size unknown",
		},
	}
	for name, tc := range cases {
		s, err := Parse(tc.sheet)
		if err != nil {
			t.Fatalf("%s: Parse: %v", name, err)
		}
		if _, err := BuildPlan(s, tc.sizes); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		} else if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q lacks %q", name, err, tc.want)
		}
	}
}

func TestWriteVCDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bin := makeSectors(6)
	if err := os.WriteFile(filepath.Join(dir, "g.bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Parse(twoTrackSheet)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, err := BuildPlan(s, map[string]int64{"g.bin": int64(len(bin))})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	var out bytes.Buffer
	n, err := WriteVCD(p, dir, &out)
	if err != nil {
		t.Fatalf("WriteVCD: %v", err)
	}
	got := out.Bytes()
	if n != int64(len(got)) || len(got) != 6*SectorSizeVCD {
		t.Fatalf("wrote %d bytes, buffer %d, want %d", n, len(got), 6*SectorSizeVCD)
	}
	if !bytes.Equal(got[0:4*SectorSizeVCD], bin[0:4*SectorSizeVCD]) {
		t.Error("track 1 data mismatch")
	}
	if !bytes.Equal(got[5*SectorSizeVCD:], bin[5*SectorSizeVCD:]) {
		t.Error("track 2 data mismatch")
	}
	gap := got[4*SectorSizeVCD : 5*SectorSizeVCD]
	if !bytes.Equal(gap, make([]byte, SectorSizeVCD)) {
		t.Error("pregap region is not zero-filled")
	}
}

func TestWriteVCDToFile(t *testing.T) {
	dir := t.TempDir()
	bin := makeSectors(2)
	if err := os.WriteFile(filepath.Join(dir, "g.bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Parse("FILE \"g.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, err := BuildPlan(s, map[string]int64{"g.bin": int64(len(bin))})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "out.vcd"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteVCD(p, dir, f); err != nil {
		f.Close()
		t.Fatalf("WriteVCD: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	back, err := os.ReadFile(filepath.Join(dir, "out.vcd"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, bin) {
		t.Error("file output differs from source BIN")
	}
}

func TestWriteVCDTruncatedBIN(t *testing.T) {
	dir := t.TempDir()
	// Plan claims 2 sectors; file holds 1.
	if err := os.WriteFile(filepath.Join(dir, "g.bin"), makeSectors(1), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Plan{Ops: []Op{
		{Kind: OpCopy, File: "g.bin", Offset: 0, Length: 2 * SectorSizeVCD},
	}}
	var out bytes.Buffer
	if _, err := WriteVCD(p, dir, &out); err == nil {
		t.Error("expected truncation error, got nil")
	} else if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error %q lacks 'truncated'", err)
	}
}

func TestWriteVCDLargeSparse(t *testing.T) {
	if testing.Short() {
		t.Skip("skip 1 GiB streaming test in short mode")
	}
	dir := t.TempDir()
	const sectors = (1 << 30) / SectorSizeVCD // ~1 GiB, truncated to whole sectors
	size := int64(sectors) * SectorSizeVCD
	f, err := os.Create(filepath.Join(dir, "big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil { // sparse: no real disk used
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	s, err := Parse("FILE \"big.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	p, err := BuildPlan(s, map[string]int64{"big.bin": size})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	n, err := WriteVCD(p, dir, io.Discard)
	if err != nil {
		t.Fatalf("WriteVCD: %v", err)
	}
	if n != size {
		t.Errorf("wrote %d bytes, want %d", n, size)
	}
}

func TestWriteVCDPathTraversal(t *testing.T) {
	for _, name := range []string{"../evil.bin", "/abs/evil.bin"} {
		p := &Plan{Ops: []Op{
			{Kind: OpCopy, File: name, Offset: 0, Length: SectorSizeVCD},
		}}
		var out bytes.Buffer
		if _, err := WriteVCD(p, t.TempDir(), &out); err == nil {
			t.Errorf("%q: expected escape error, got nil", name)
		}
	}
}
