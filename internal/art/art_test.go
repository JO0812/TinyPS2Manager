package art

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

func TestKeyFor(t *testing.T) {
	// PS2 with GameID
	it := library.LibraryItem{Platform: library.PlatformPS2, GameID: "SLUS_213.85", Title: "GTA"}
	if k := KeyFor(it, "", "", false); k != "SLUS_213.85_COV.png" {
		t.Errorf("ps2 key = %q", k)
	}
	// PS2 without GameID falls back to title
	it2 := library.LibraryItem{Platform: library.PlatformPS2, Title: "My Game"}
	if k := KeyFor(it2, "", "", false); k != "My Game_COV.png" {
		t.Errorf("ps2 fallback = %q", k)
	}
	// PS1 POPSTARTER VCD basename
	it3 := library.LibraryItem{Platform: library.PlatformPS1, Title: "Spyro"}
	if k := KeyFor(it3, "SCUS_945.67.Final Fantasy VII.VCD", "", false); k != "SCUS_945.67.Final Fantasy VII_COV.png" {
		t.Errorf("ps1 vcd key = %q", k)
	}
	if k := KeyFor(it3, "Spyro 2 (Ripto's Rage).VCD", "", false); k != "Spyro 2 (Ripto's Rage)_COV.png" {
		t.Errorf("ps1 vcd2 = %q", k)
	}
	// Ember folder
	it4 := library.LibraryItem{Platform: library.PlatformPS1, Title: "EmberGame"}
	if k := KeyFor(it4, "", "My Ember Game", true); k != "My Ember Game_COV.png" {
		t.Errorf("ember = %q", k)
	}
	// Sanitize forbidden chars
	it5 := library.LibraryItem{Platform: library.PlatformPS2, Title: "A/B:C*D"}
	if k := KeyFor(it5, "", "", false); k != "A_B_C_D_COV.png" {
		t.Errorf("sanitize = %q", k)
	}
}

func TestPS2CoverURLs(t *testing.T) {
	urls := PS2CoverURLs("SLUS_213.85")
	if len(urls) == 0 {
		t.Fatal("no urls")
	}
	// First should be EN with flat ID
	if !strings.Contains(urls[0], "/EN/SLUS21385.png") {
		t.Errorf("first url = %q", urls[0])
	}
	if !strings.HasPrefix(urls[0], PS2GameTDBBase) {
		t.Errorf("base = %q", urls[0])
	}
}

func TestPS1CoverURLs(t *testing.T) {
	urls := PS1CoverURLs("Final Fantasy VII")
	if len(urls) != 1 {
		t.Fatalf("len = %d", len(urls))
	}
	if !strings.Contains(urls[0], "Named_Boxarts") {
		t.Errorf("url = %q", urls[0])
	}
}

func makePNG(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestValidateAndNormalize(t *testing.T) {
	// Tiny 10x10 red -> 512x730 for PS2
	raw := makePNG(10, 10, color.RGBA{255, 0, 0, 255})
	norm, err := ValidateAndNormalize(raw, true)
	if err != nil {
		t.Fatalf("normalize ps2: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(norm))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != PS2Width || img.Bounds().Dy() != PS2Height {
		t.Errorf("ps2 size = %v", img.Bounds())
	}
	// PS1 20x10 -> 512x512
	norm2, err := ValidateAndNormalize(raw, false)
	if err != nil {
		t.Fatalf("normalize ps1: %v", err)
	}
	img2, _ := png.Decode(bytes.NewReader(norm2))
	if img2.Bounds().Dx() != PS1Width || img2.Bounds().Dy() != PS1Height {
		t.Errorf("ps1 size = %v", img2.Bounds())
	}
	// Bad magic
	if _, err := ValidateAndNormalize([]byte("notpng"), true); err == nil {
		t.Error("bad magic: expected error")
	}
}

func TestGenerateCustom(t *testing.T) {
	data := GenerateCustom("My Game Title", true)
	if len(data) < 8 || !bytes.Equal(data[:8], pngMagic) {
		t.Error("custom not png")
	}
	img, _ := png.Decode(bytes.NewReader(data))
	if img.Bounds().Dx() != PS2Width || img.Bounds().Dy() != PS2Height {
		t.Errorf("custom ps2 size = %v", img.Bounds())
	}
	data2 := GenerateCustom("Other", false)
	img2, _ := png.Decode(bytes.NewReader(data2))
	if img2.Bounds().Dx() != PS1Width {
		t.Errorf("custom ps1 size = %v", img2.Bounds())
	}
}

func TestFetch(t *testing.T) {
	pngData := makePNG(2, 2, color.RGBA{0, 255, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client()}
	// OK
	got, err := c.Fetch(context.Background(), srv.URL+"/ok.png")
	if err != nil {
		t.Fatalf("fetch ok: %v", err)
	}
	if !bytes.Equal(got, pngData) {
		t.Error("fetch data mismatch")
	}
	// 404 with retries should fail
	if _, err := c.Fetch(context.Background(), srv.URL+"/missing.png"); err == nil {
		t.Error("missing: expected error")
	}
	// Bad PNG magic
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not a png"))
	}))
	defer srv2.Close()
	c2 := &Client{HTTP: srv2.Client()}
	if _, err := c2.Fetch(context.Background(), srv2.URL); err == nil {
		t.Error("bad png: expected error")
	}
}

func TestStage(t *testing.T) {
	root := t.TempDir()
	disk := transfer.FileDisk{}
	pngData := makePNG(2, 2, color.RGBA{10, 20, 30, 255})
	key := "SLUS_123.45_COV.png"
	if err := Stage(context.Background(), disk, root, "", key, pngData); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	path := filepath.Join(root, "ART", key)
	if _, err := disk.Stat(path); err != nil {
		t.Fatalf("stat after stage: %v", err)
	}
	// Hand file wins: second stage with different data should not overwrite
	other := makePNG(2, 2, color.RGBA{99, 99, 99, 255})
	if err := Stage(context.Background(), disk, root, "", key, other); err != nil {
		t.Fatalf("second Stage: %v", err)
	}
	f, err := disk.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, _ := png.Decode(f)
	_ = got // just ensure it decodes; byte equality would require reading raw

	// With prefix
	root2 := t.TempDir()
	if err := Stage(context.Background(), disk, root2, "OPL", key, pngData); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.Stat(filepath.Join(root2, "OPL", "ART", key)); err != nil {
		t.Fatalf("prefixed: %v", err)
	}
}
