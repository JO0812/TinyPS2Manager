package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/jo/TinyPS2Manager/internal/art"
	"github.com/jo/TinyPS2Manager/internal/library"
)

// tinyPNG is a minimal 1x1 transparent PNG (67 bytes).
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func coverURL(t *testing.T, id int64, dest string) string {
	t.Helper()
	return fmt.Sprintf("/api/library/%d/cover?destinationPath=%s", id, url.QueryEscape(dest))
}

func TestLibraryCover(t *testing.T) {
	h, cancel, destDir, items := prepareHarness(t)
	defer cancel()
	if len(items) == 0 {
		t.Fatal("harness imported no items")
	}
	it := items[0]
	key := art.KeyFor(library.LibraryItem{Platform: library.PlatformPS2, Title: it.Title}, "", "", false)
	artDir := filepath.Join(destDir, "ART")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artDir, key), tinyPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	// Staged cover streams back as image/png with PNG magic intact.
	code, raw := h.do("GET", coverURL(t, it.ID, destDir), nil)
	if code != http.StatusOK {
		t.Fatalf("cover = %d\n%s", code, raw)
	}
	if !bytes.Equal(raw, tinyPNG) {
		t.Fatalf("cover bytes differ: got %d bytes", len(raw))
	}
	req, _ := http.NewRequest("GET", h.base+coverURL(t, it.ID, destDir), nil)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q", ct)
	}

	// Missing cover → 404.
	if err := os.Remove(filepath.Join(artDir, key)); err != nil {
		t.Fatal(err)
	}
	if code, raw := h.do("GET", coverURL(t, it.ID, destDir), nil); code != http.StatusNotFound {
		t.Fatalf("missing cover = %d\n%s", code, raw)
	}

	// Non-PNG file under the cover name → 422.
	if err := os.WriteFile(filepath.Join(artDir, key), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, raw := h.do("GET", coverURL(t, it.ID, destDir), nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("bogus cover = %d\n%s", code, raw)
	}

	// Unknown item → 404; unregistered destination → 404; missing
	// destinationPath → 400.
	if code, _ := h.do("GET", coverURL(t, 999999, destDir), nil); code != http.StatusNotFound {
		t.Fatalf("unknown item = %d", code)
	}
	if code, _ := h.do("GET", coverURL(t, it.ID, filepath.Join(destDir, "nope")), nil); code != http.StatusNotFound {
		t.Fatalf("unknown dest = %d", code)
	}
	if code, _ := h.do("GET", fmt.Sprintf("/api/library/%d/cover", it.ID), nil); code != http.StatusBadRequest {
		t.Fatalf("missing dest param = %d", code)
	}
}
