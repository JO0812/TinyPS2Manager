package riptopl

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jo/TinyPS2Manager/internal/transfer"
)

// fixturePackage builds a package zip with two flavours plus decoys.
func fixturePackage(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	add := func(name, content string) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	add("APPS/APP_RIPTOPL-PS2DEVPINNED/RIPTOPL.ELF", "pinned-elf")
	add("APPS/APP_RIPTOPL-OFFICIALROLLING/RIPTOPL.ELF", "rolling-elf")
	add("ART/README.txt", "decoy")
	add("POPS/POPSTARTER.ELF", "decoy")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fixtureServer serves a fake GitHub releases API for tag lookups and asset
// downloads. It returns the server and the package zip bytes.
func fixtureServer(t *testing.T) (*httptest.Server, []byte, string) {
	t.Helper()
	pkg := fixturePackage(t)
	sum := sha256.Sum256(pkg)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/NathanNeurotic/Open-PS2-Loader/releases/tags/current-fan-favorite", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":         "current-fan-favorite",
			"target_commitish": "abc123",
			"published_at":     "2026-09-04T15:58:03Z",
			"assets": []map[string]any{
				{"name": "RIPTOPL-LANGS-v1.zip", "browser_download_url": "http://" + r.Host + "/dl/langs.zip", "size": 10, "digest": digest},
				{"name": "RIPTOPL-v1.zip", "browser_download_url": "http://" + r.Host + "/dl/pkg.zip", "size": len(pkg), "digest": digest},
			},
		})
	})
	mux.HandleFunc("/repos/NathanNeurotic/Open-PS2-Loader/releases/tags/empty", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "empty", "assets": []map[string]any{
				{"name": "RIPTOPL-LANGS-v1.zip", "browser_download_url": "http://" + r.Host + "/dl/langs.zip", "size": 10, "digest": digest},
			},
		})
	})
	mux.HandleFunc("/dl/pkg.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(pkg)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, pkg, digest
}

func testClient(srv *httptest.Server) *Client {
	return &Client{APIBase: srv.URL, HTTP: srv.Client()}
}

func TestResolve(t *testing.T) {
	srv, pkg, digest := fixtureServer(t)
	c := testClient(srv)
	rel, err := c.Resolve(context.Background(), "current-fan-favorite")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rel.AssetName != "RIPTOPL-v1.zip" || rel.Commit != "abc123" {
		t.Errorf("release = %+v", rel)
	}
	if !strings.HasPrefix(rel.AssetURL, srv.URL+"/dl/pkg.zip") {
		t.Errorf("url = %q", rel.AssetURL)
	}
	if rel.AssetSize != int64(len(pkg)) {
		t.Errorf("size = %d", rel.AssetSize)
	}
	wantDigest := strings.TrimPrefix(digest, "sha256:")
	if rel.Digest != wantDigest {
		t.Errorf("digest = %q", rel.Digest)
	}
	if _, err := c.Resolve(context.Background(), "empty"); err == nil {
		t.Error("package-less tag: expected error")
	}
	if _, err := c.Resolve(context.Background(), "nope"); err == nil {
		t.Error("missing tag: expected error")
	}
}

func TestDownload(t *testing.T) {
	srv, pkg, _ := fixtureServer(t)
	c := testClient(srv)
	rel, err := c.Resolve(context.Background(), "current-fan-favorite")
	if err != nil {
		t.Fatal(err)
	}
	var progress int64
	path, err := c.Download(context.Background(), rel, func(done, total int64) { progress = done })
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer os.Remove(path)
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, pkg) {
		t.Error("downloaded bytes differ")
	}
	if progress != int64(len(pkg)) {
		t.Errorf("progress = %d", progress)
	}
	// Tampered digest fails closed.
	rel.Digest = strings.Repeat("0", 64)
	if _, err := c.Download(context.Background(), rel, nil); err == nil {
		t.Error("tampered digest: expected error")
	}
}

func TestStage(t *testing.T) {
	srv, _, _ := fixtureServer(t)
	c := testClient(srv)
	rel, err := c.Resolve(context.Background(), "current-fan-favorite")
	if err != nil {
		t.Fatal(err)
	}
	zipPath, err := c.Download(context.Background(), rel, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(zipPath)
	root := t.TempDir()
	st, err := Stage(context.Background(), transfer.FileDisk{}, root, "", zipPath, nil)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if st.Flavour != "APP_RIPTOPL-PS2DEVPINNED" {
		t.Errorf("flavour = %q (preference order broken)", st.Flavour)
	}
	want := filepath.Join(root, "APPS", "APP_RIPTOPL-PS2DEVPINNED", "RIPTOPL.ELF")
	if st.ELFPath != want {
		t.Errorf("path = %q", st.ELFPath)
	}
	back, err := os.ReadFile(want)
	if err != nil || string(back) != "pinned-elf" {
		t.Errorf("staged = %q, %v", back, err)
	}
	// Prefix nests the whole layout.
	root2 := t.TempDir()
	st2, err := Stage(context.Background(), transfer.FileDisk{}, root2, "OPL", zipPath, []string{"APP_RIPTOPL-OFFICIALROLLING"})
	if err != nil {
		t.Fatal(err)
	}
	if st2.Flavour != "APP_RIPTOPL-OFFICIALROLLING" {
		t.Errorf("explicit flavour ignored: %q", st2.Flavour)
	}
	if _, err := os.Stat(filepath.Join(root2, "OPL", "APPS", "APP_RIPTOPL-OFFICIALROLLING", "RIPTOPL.ELF")); err != nil {
		t.Errorf("prefixed stage: %v", err)
	}
	// Unknown flavour fails closed.
	if _, err := Stage(context.Background(), transfer.FileDisk{}, t.TempDir(), "", zipPath, []string{"NOPE"}); err == nil {
		t.Error("unknown flavour: expected error")
	}
}

func TestStageZipSlip(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("../evil.elf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("evil"))
	f2, err := w.Create("APPS/APP_RIPTOPL-PS2DEVPINNED/RIPTOPL.ELF")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f2.Write([]byte("good"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	// findFlavourELF matches the exact entry; the slip entry is ignored.
	if _, err := Stage(context.Background(), transfer.FileDisk{}, root, "", zipPath, nil); err != nil {
		t.Fatalf("Stage with slip entry present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "evil.elf")); !os.IsNotExist(err) {
		t.Error("zip-slip file escaped")
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "..", "evil.elf")); !os.IsNotExist(err) {
		t.Error("zip-slip file escaped to parent")
	}
}

func TestChecklist(t *testing.T) {
	steps := Checklist()
	if len(steps) < 3 {
		t.Fatalf("checklist too short: %v", steps)
	}
	joined := strings.Join(steps, "\n")
	for _, want := range []string{"Game Sources", "Save Changes", "L3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("checklist lacks %q", want)
		}
	}
}

func TestIsPackageAsset(t *testing.T) {
	for _, good := range []string{"RIPTOPL-v1.2.0-Beta-3065.zip", "RIPTOPL-v1.zip"} {
		if !isPackageAsset(good) {
			t.Errorf("%q should match", good)
		}
	}
	for _, bad := range []string{
		"RIPTOPL-DEBUG-v1.zip", "RIPTOPL-VARIANTS-v1.zip", "RIPTOPL-LANGS-v1.zip",
		"RIPTOPL-RA.ELF", "RIPTOPL.ELF", "RIPTOPL-v1-src.zip", "APP_RIPTOPL.psu",
	} {
		if isPackageAsset(bad) {
			t.Errorf("%q should not match", bad)
		}
	}
}
