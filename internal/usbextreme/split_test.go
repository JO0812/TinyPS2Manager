package usbextreme

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testChunkSize = 1 << 20 // 1 MiB stands in for 1 GiB in tests

func TestChunkSizeConstant(t *testing.T) {
	if ChunkSize != 1073741824 {
		t.Errorf("ChunkSize = %d, want published 1073741824 (1 GiB)", ChunkSize)
	}
}

func TestGameCRC(t *testing.T) {
	// Vectors from ulmake's test suite (MIT) — byte-exact compat proof.
	cases := map[string]string{
		"f":                                "8433E5CC",
		"F":                                "A490D3EA",
		"fooooooooooooooooooooooooooooooo": "84BA9D95",
		"FOOOOOOOOOOOOOOOOOOOOOOOOOOOOOOO": "8CAF0142",
	}
	for name, want := range cases {
		if got := GameCRC(name); got != want {
			t.Errorf("GameCRC(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestChunkName(t *testing.T) {
	got := ChunkName("fooooooooooooooooooooooooooooooo", "SLXS_123.45", 0)
	if got != "ul.84BA9D95.SLXS_123.45.00" {
		t.Errorf("got %q", got)
	}
	if got := ChunkName("Game", "SLUS_111.11", 3); !strings.HasSuffix(got, ".03") {
		t.Errorf("got %q", got)
	}
}

func randomBytes(t *testing.T, n int64) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := randomBytes(t, 2*testChunkSize+12345)
	sum := sha256.Sum256(src)
	n, err := writeWithChunkSize(dir, "Test Game", "SLUS_111.11", bytes.NewReader(src), int64(len(src)), testChunkSize)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 3 {
		t.Fatalf("chunks = %d, want 3", n)
	}
	// Record layout assertions (64-byte ul.cfg entry).
	raw, err := os.ReadFile(filepath.Join(dir, "ul.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 64 {
		t.Fatalf("ul.cfg = %d bytes, want 64", len(raw))
	}
	if name := stripNULs(raw[0:32]); name != "Test Game" {
		t.Errorf("name field = %q", name)
	}
	if string(raw[32:35]) != "ul." {
		t.Errorf("prefix = %q", raw[32:35])
	}
	if serial := stripNULs(raw[35:47]); serial != "SLUS_111.11" {
		t.Errorf("serial field = %q", serial)
	}
	if raw[47] != 3 || raw[48] != 0x14 || raw[53] != 0x08 {
		t.Errorf("count/magic = %02x %02x %02x", raw[47], raw[48], raw[53])
	}
	// Chunk files on disk.
	for i, want := range []int64{testChunkSize, testChunkSize, 12345} {
		fi, err := os.Stat(filepath.Join(dir, ChunkName("Test Game", "SLUS_111.11", i)))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Size() != want {
			t.Errorf("chunk %d = %d, want %d", i, fi.Size(), want)
		}
	}
	// Read back through the verification reader.
	r, total, err := openWithChunkSize(dir, "SLUS_111.11", testChunkSize)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	back, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if total != int64(len(src)) || sha256.Sum256(back) != sum {
		t.Error("round-trip content mismatch")
	}
	if err := verifyWithChunkSize(dir, "SLUS_111.11", testChunkSize); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestWriteBoundaries(t *testing.T) {
	for _, size := range []int64{1, testChunkSize, testChunkSize + 1} {
		dir := t.TempDir()
		src := randomBytes(t, size)
		n, err := writeWithChunkSize(dir, "G", "SLUS_111.11", bytes.NewReader(src), size, testChunkSize)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		want := int((size + testChunkSize - 1) / testChunkSize)
		if n != want {
			t.Errorf("size %d: chunks %d, want %d", size, n, want)
		}
	}
}

func TestWriteUpsertReplacesStale(t *testing.T) {
	dir := t.TempDir()
	a1 := randomBytes(t, 2*testChunkSize+10)
	if _, err := writeWithChunkSize(dir, "Game A", "SLUS_111.11", bytes.NewReader(a1), int64(len(a1)), testChunkSize); err != nil {
		t.Fatal(err)
	}
	b := randomBytes(t, 100)
	if _, err := writeWithChunkSize(dir, "Game B", "SLUS_222.22", bytes.NewReader(b), int64(len(b)), testChunkSize); err != nil {
		t.Fatal(err)
	}
	oldChunk := ChunkName("Game A", "SLUS_111.11", 1)
	if _, err := os.Stat(filepath.Join(dir, oldChunk)); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Rewrite A smaller under a new name: old chunks go, record updates.
	a2 := randomBytes(t, 100)
	if _, err := writeWithChunkSize(dir, "Game A GOTY", "SLUS_111.11", bytes.NewReader(a2), int64(len(a2)), testChunkSize); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, oldChunk)); !os.IsNotExist(err) {
		t.Errorf("stale chunk %s survives", oldChunk)
	}
	entries, err := List(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("List = %v, %v", entries, err)
	}
	for _, e := range entries {
		if e.Serial == "SLUS_111.11" && (e.OPLName != "Game A GOTY" || e.Chunks != 1) {
			t.Errorf("record not replaced: %+v", e)
		}
	}
	r, _, err := openWithChunkSize(dir, "SLUS_111.11", testChunkSize)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(back, a2) {
		t.Error("rewritten game reads back stale content")
	}
}

func TestWriteErrors(t *testing.T) {
	ok := randomBytes(t, 100)
	cases := map[string]struct {
		name   string
		serial string
		src    []byte
		size   int64
	}{
		"bad serial":  {"G", "nope", ok, 100},
		"empty name":  {"", "SLUS_111.11", ok, 100},
		"long name":   {strings.Repeat("A", 33), "SLUS_111.11", ok, 100},
		"zero size":   {"G", "SLUS_111.11", ok, 0},
		"short src":   {"G", "SLUS_111.11", ok[:50], 100},
		"long src":    {"G", "SLUS_111.11", ok, 50},
		"oversize":    {"G", "SLUS_111.11", ok, maxChunks*testChunkSize + 1},
	}
	for name, tc := range cases {
		dir := t.TempDir()
		if _, err := writeWithChunkSize(dir, tc.name, tc.serial, bytes.NewReader(tc.src), tc.size, testChunkSize); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
		if entries, _ := List(dir); len(entries) != 0 {
			t.Errorf("%s: failed write left ul.cfg entries", name)
		}
	}
}

func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := openWithChunkSize(dir, "SLUS_111.11", testChunkSize); err == nil {
		t.Error("missing record: expected error")
	}
	src := randomBytes(t, 2*testChunkSize)
	if _, err := writeWithChunkSize(dir, "G", "SLUS_111.11", bytes.NewReader(src), int64(len(src)), testChunkSize); err != nil {
		t.Fatal(err)
	}
	// Delete a chunk.
	if err := os.Remove(filepath.Join(dir, ChunkName("G", "SLUS_111.11", 1))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openWithChunkSize(dir, "SLUS_111.11", testChunkSize); err == nil {
		t.Error("missing chunk: expected error")
	}
	if err := verifyWithChunkSize(dir, "SLUS_111.11", testChunkSize); err == nil {
		t.Error("verify with missing chunk: expected error")
	}
	// Corrupt the count byte.
	raw, _ := os.ReadFile(filepath.Join(dir, "ul.cfg"))
	raw[47] = 9
	os.WriteFile(filepath.Join(dir, "ul.cfg"), raw, 0o644)
	if _, _, err := openWithChunkSize(dir, "SLUS_111.11", testChunkSize); err == nil {
		t.Error("count mismatch: expected error")
	}
}

func TestListCorrupt(t *testing.T) {
	dir := t.TempDir()
	if entries, err := List(dir); err != nil || len(entries) != 0 {
		t.Errorf("missing ul.cfg = %v, %v", entries, err)
	}
	os.WriteFile(filepath.Join(dir, "ul.cfg"), make([]byte, 100), 0o644)
	if _, err := List(dir); err == nil {
		t.Error("ragged ul.cfg: expected error")
	}
	os.WriteFile(filepath.Join(dir, "ul.cfg"), make([]byte, 64), 0o644)
	if _, err := List(dir); err == nil {
		t.Error("empty record: expected error")
	}
}
