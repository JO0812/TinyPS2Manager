package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRiptopl serves release metadata + a tiny package zip. digestOut
// receives the served zip's digest for assertions.
func fakeRiptopl(t *testing.T, digestOut *string) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("APP_RIPTOPL-PS2DEVPINNED/RIPTOPL.ELF")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("test-elf")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	pkg := buf.Bytes()
	sum := sha256.Sum256(pkg)
	*digestOut = hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/NathanNeurotic/Open-PS2-Loader/releases/tags/current-fan-favorite", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "current-fan-favorite", "target_commitish": "abc",
			"published_at": "2026-01-01T00:00:00Z",
			"assets": []map[string]any{
				{"name": "RIPTOPL-v1.zip", "browser_download_url": "http://" + r.Host + "/dl/pkg.zip",
					"size": len(pkg), "digest": "sha256:" + *digestOut},
			},
		})
	})
	mux.HandleFunc("/dl/pkg.zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pkg)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func prepareHarness(t *testing.T) (*apiHarness, context.CancelFunc, string, []libraryItemJSON) {
	t.Helper()
	h, cancel := newAPIHarness(t)
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "small.iso"), []byte(strings.Repeat("i", 10000)), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := make([]byte, 0, 2*2352)
	for i := 0; i < 2; i++ {
		for j := 0; j < 2352; j++ {
			bin = append(bin, byte(i+1))
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "g.bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	cue := "FILE \"g.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"
	if err := os.WriteFile(filepath.Join(srcDir, "g.cue"), []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()
	var dest destinationJSON
	if code, raw := h.do("POST", "/api/destinations",
		map[string]string{"path": destDir}); code != http.StatusCreated {
		t.Fatalf("dest = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &dest); err != nil {
		t.Fatal(err)
	}
	if code, _ := h.do("PATCH", fmt.Sprintf("/api/destinations/%d", dest.ID),
		map[string]string{"filesystemOverride": "exfat"}); code != http.StatusOK {
		t.Fatalf("override = %d", code)
	}
	var items []libraryItemJSON
	if code, raw := h.do("POST", "/api/library/import",
		map[string]string{"path": srcDir}); code != http.StatusCreated {
		t.Fatalf("import = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	return h, cancel, destDir, items
}

func idsOf(items []libraryItemJSON) []int64 {
	var out []int64
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestPreparePreview(t *testing.T) {
	h, cancel, destDir, items := prepareHarness(t)
	defer cancel()
	var dests []destinationJSON
	if code := h.get("/api/destinations", &dests); code != http.StatusOK || len(dests) != 1 {
		t.Fatalf("dests = %+v", dests)
	}
	destID := dests[0].ID

	var pv preparePreview
	if code, raw := h.do("POST", fmt.Sprintf("/api/destinations/%d/prepare", destID),
		map[string]any{"mode": "preview", "itemIds": idsOf(items)}); code != http.StatusOK {
		t.Fatalf("preview = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &pv); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(append(pv.Dirs, pv.Files...), "\n")
	for _, want := range []string{"CD", "small.iso", "POPS", "g.VCD"} {
		if !strings.Contains(joined, want) {
			t.Errorf("preview lacks %q:\n%s", want, joined)
		}
	}
	if pv.Riptopl != nil {
		t.Error("no tag given, yet riptopl resolved")
	}
	if len(pv.Checklist) == 0 {
		t.Error("checklist missing")
	}
	// Nothing written by a preview.
	if _, err := os.Stat(filepath.Join(destDir, "CD")); !os.IsNotExist(err) {
		t.Error("preview created directories")
	}
	if code, _ := h.do("POST", fmt.Sprintf("/api/destinations/%d/prepare", destID),
		map[string]any{"mode": "bogus"}); code != http.StatusBadRequest {
		t.Fatalf("bad mode = %d, want 400", code)
	}
}

func TestPrepareExecuteWithLoader(t *testing.T) {
	h, cancel, destDir, items := prepareHarness(t)
	defer cancel()
	var digest string
	fake := fakeRiptopl(t, &digest)
	h.setRiptoplBase(t, fake.URL)

	var dests []destinationJSON
	if code := h.get("/api/destinations", &dests); code != http.StatusOK || len(dests) != 1 {
		t.Fatalf("dests = %+v", dests)
	}
	destID := dests[0].ID

	var res prepareResult
	if code, raw := h.do("POST", fmt.Sprintf("/api/destinations/%d/prepare", destID),
		map[string]any{"mode": "execute", "itemIds": idsOf(items), "riptoplTag": "current-fan-favorite"}); code != http.StatusOK {
		t.Fatalf("execute = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.Riptopl == nil {
		t.Fatal("no staged loader in response")
	}
	if res.Riptopl.Flavour != "APP_RIPTOPL-PS2DEVPINNED" || res.Riptopl.Digest != digest {
		t.Errorf("loader = %+v", res.Riptopl)
	}
	back, err := os.ReadFile(filepath.Join(destDir, "APPS", "APP_RIPTOPL-PS2DEVPINNED", "RIPTOPL.ELF"))
	if err != nil || string(back) != "test-elf" {
		t.Errorf("staged ELF = %q, %v", back, err)
	}
	// Pinned version recorded in queue_state.
	got, ok, err := h.qstore().GetState(fmt.Sprintf("loader.%d", destID))
	if err != nil || !ok || !strings.Contains(got, digest) {
		t.Errorf("loader record = %q,%v,%v", got, ok, err)
	}
	if len(res.Jobs) != 2 {
		t.Fatalf("jobs = %+v", res.Jobs)
	}
	if len(res.Checklist) == 0 {
		t.Error("checklist missing")
	}
	// Bad tag fails closed with 502 and stages nothing new.
	if code, raw := h.do("POST", fmt.Sprintf("/api/destinations/%d/prepare", destID),
		map[string]any{"mode": "execute", "itemIds": idsOf(items), "riptoplTag": "nope"}); code != http.StatusBadGateway {
		t.Fatalf("bad tag = %d, want 502\n%s", code, raw)
	}
}

func TestPrepareExecuteNoLoader(t *testing.T) {
	h, cancel, destDir, items := prepareHarness(t)
	defer cancel()
	var dests []destinationJSON
	if code := h.get("/api/destinations", &dests); code != http.StatusOK || len(dests) != 1 {
		t.Fatalf("dests = %+v", dests)
	}
	var res prepareResult
	if code, raw := h.do("POST", fmt.Sprintf("/api/destinations/%d/prepare", dests[0].ID),
		map[string]any{"mode": "execute", "itemIds": idsOf(items)}); code != http.StatusOK {
		t.Fatalf("execute = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.Riptopl != nil {
		t.Error("loader staged without a tag")
	}
	if len(res.Jobs) != 2 {
		t.Fatalf("jobs = %+v", res.Jobs)
	}
	if _, err := os.Stat(filepath.Join(destDir, "CD")); err != nil {
		t.Errorf("tree dirs not created: %v", err)
	}
	// Unknown item fails before anything writes.
	if code, _ := h.do("POST", fmt.Sprintf("/api/destinations/%d/prepare", dests[0].ID),
		map[string]any{"mode": "execute", "itemIds": []int64{9999}}); code != http.StatusNotFound {
		t.Fatalf("unknown item = %d, want 404", code)
	}
}
