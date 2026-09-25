package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

type apiHarness struct {
	t      *testing.T
	server *httptest.Server
	srv    *Server
	base   string
	client *http.Client
}

func newAPIHarness(t *testing.T) (*apiHarness, context.CancelFunc) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "t.db")
	qs, err := queue.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { qs.Close() })
	ls, err := library.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ls.Close() })
	settings := filepath.Join(t.TempDir(), "settings.json")
	ex := queue.New(qs, ls, transfer.FileDisk{}, t.TempDir())
	srv := New(qs, ls, settings, ex)
	ctx, cancel := context.WithCancel(context.Background())
	srv.StartExecutor(ctx)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &apiHarness{t: t, server: ts, srv: srv, base: ts.URL, client: ts.Client()}, cancel
}

func (h *apiHarness) setRiptoplBase(t *testing.T, base string) {
	t.Helper()
	h.srv.WithRiptoplBase(base)
}

func (h *apiHarness) qstore() *queue.Store {
	return h.srv.qstore
}

func (h *apiHarness) do(method, path string, body any) (int, []byte) {
	h.t.Helper()
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.base+path, rdr)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		h.t.Fatal(err)
	}
	return resp.StatusCode, buf.Bytes()
}

func (h *apiHarness) get(path string, v any) int {
	h.t.Helper()
	code, raw := h.do("GET", path, nil)
	if v != nil {
		if err := json.Unmarshal(raw, v); err != nil {
			h.t.Fatalf("GET %s: %v\n%s", path, err, raw)
		}
	}
	return code
}

func buildFixtures(t *testing.T, dir string) {
	t.Helper()
	mk := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("small.iso", strings.Repeat("i", 10000))
	bin := make([]byte, 0, 6*2352)
	for i := 0; i < 6; i++ {
		for j := 0; j < 2352; j++ {
			bin = append(bin, byte(i+1))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "g.bin"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	mk("g.cue", "FILE \"g.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"+
		"  TRACK 02 AUDIO\n    INDEX 00 00:00:04\n    INDEX 01 00:00:05\n")
	multi := filepath.Join(dir, "multi")
	if err := os.MkdirAll(multi, 0o755); err != nil {
		t.Fatal(err)
	}
	sector := make([]byte, 2352)
	for i := range sector {
		sector[i] = byte(i)
	}
	for i := 1; i <= 5; i++ {
		name := fmt.Sprintf("Epic (Disc %d)", i)
		if err := os.WriteFile(filepath.Join(multi, name+".bin"), sector, 0o644); err != nil {
			t.Fatal(err)
		}
		cue := fmt.Sprintf("FILE %q BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n", name+".bin")
		if err := os.WriteFile(filepath.Join(multi, name+".cue"), []byte(cue), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIntegrationEndToEnd(t *testing.T) {
	h, cancel := newAPIHarness(t)
	defer cancel()
	srcDir := t.TempDir()
	buildFixtures(t, srcDir)
	destDir := t.TempDir()

	// Destination with explicit exFAT (tmpfs probes unknown).
	var dest destinationJSON
	if code, raw := h.do("POST", "/api/destinations",
		map[string]string{"path": destDir}); code != http.StatusCreated {
		t.Fatalf("create dest = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &dest); err != nil {
		t.Fatal(err)
	}
	if code, _ := h.do("PATCH", fmt.Sprintf("/api/destinations/%d", dest.ID),
		map[string]string{"filesystemOverride": "exfat", "bdmPrefix": ""}); code != http.StatusOK {
		t.Fatalf("patch dest = %d", code)
	}
	if code, _ := h.do("PATCH", "/api/destinations/9999",
		map[string]string{"bdmPrefix": "X"}); code != http.StatusNotFound {
		t.Fatalf("missing dest = %d, want 404", code)
	}

	// Import + library reads.
	var items []libraryItemJSON
	if code, raw := h.do("POST", "/api/library/import",
		map[string]string{"path": srcDir}); code != http.StatusCreated {
		t.Fatalf("import = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	var listed []libraryItemJSON
	if code := h.get("/api/library", &listed); code != http.StatusOK || len(listed) == 0 {
		t.Fatalf("library = %d items %d", code, len(listed))
	}
	byTitle := map[string]libraryItemJSON{}
	for _, it := range listed {
		byTitle[it.Title] = it
	}
	iso, cue := byTitle["small"], byTitle["g"]
	if iso.DiscType != "cd" {
		t.Errorf("iso type = %q, want cd", iso.DiscType)
	}

	// PATCH title + unknown-field rejection.
	if code, _ := h.do("PATCH", fmt.Sprintf("/api/library/%d", iso.ID),
		map[string]string{"title": "Renamed"}); code != http.StatusOK {
		t.Fatalf("rename = %d", code)
	}
	if code, _ := h.do("PATCH", fmt.Sprintf("/api/library/%d", iso.ID),
		map[string]string{"bogus": "x"}); code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, want 400", code)
	}

	// Enqueue copy + convert; reorder before the executor drains.
	var jobs []jobJSON
	if code, raw := h.do("POST", "/api/queue", map[string]any{
		"destinationId": dest.ID, "itemIds": []int64{iso.ID, cue.ID},
	}); code != http.StatusCreated {
		t.Fatalf("enqueue = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].Kind != "copy" || jobs[1].Kind != "convert-and-copy" {
		t.Fatalf("kinds = %+v", jobs)
	}
	if code, _ := h.do("PATCH", fmt.Sprintf("/api/queue/%d", jobs[1].ID),
		map[string]int{"order": 0}); code != http.StatusOK {
		t.Fatalf("reorder = %d", code)
	}
	var ordered []jobJSON
	if code := h.get("/api/queue", &ordered); code != http.StatusOK ||
		ordered[0].ID != jobs[1].ID {
		t.Fatalf("order not applied: %+v", ordered)
	}

	// Drain through the running executor.
	deadline := time.Now().Add(30 * time.Second)
	for {
		var cur []jobJSON
		if code := h.get("/api/queue", &cur); code != http.StatusOK {
			t.Fatalf("queue = %d", code)
		}
		done := true
		for _, j := range cur {
			if j.Status != "done" {
				done = false
			}
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue did not drain: %+v", cur)
		}
		time.Sleep(100 * time.Millisecond)
	}
	back, err := os.ReadFile(filepath.Join(destDir, "CD", "small.iso"))
	if err != nil || len(back) != 10000 {
		t.Errorf("copied iso = %d bytes, %v", len(back), err)
	}
	vcd, err := os.ReadFile(filepath.Join(destDir, "POPS", "g.VCD"))
	if err != nil || len(vcd) != 6*2352 {
		t.Errorf("vcd = %d bytes, %v", len(vcd), err)
	}

	// Global pause + skip flow (executor running but paused: deterministic).
	if code, _ := h.do("POST", "/api/queue/pause", nil); code != http.StatusOK {
		t.Fatalf("pause = %d", code)
	}
	var extra []jobJSON
	if code, raw := h.do("POST", "/api/queue", map[string]any{
		"destinationId": dest.ID, "itemIds": []int64{iso.ID},
	}); code != http.StatusCreated {
		t.Fatalf("enqueue2 = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &extra); err != nil {
		t.Fatal(err)
	}
	if code, raw := h.do("PATCH", fmt.Sprintf("/api/queue/%d", extra[0].ID),
		map[string]string{"action": "skip"}); code != http.StatusOK {
		t.Fatalf("skip = %d\n%s", code, raw)
	}
	var after []jobJSON
	if code := h.get("/api/queue", &after); code != http.StatusOK {
		t.Fatal(code)
	}
	for _, j := range after {
		if j.ID == extra[0].ID {
			t.Fatal("skipped job survives")
		}
	}
	if code, _ := h.do("POST", "/api/queue/resume", nil); code != http.StatusOK {
		t.Fatalf("resume = %d", code)
	}

	// Settings round-trip.
	var settings map[string]any
	if code := h.get("/api/settings", &settings); code != http.StatusOK ||
		settings["theme"] != "system" {
		t.Fatalf("settings = %v", settings)
	}
	put := map[string]any{"theme": "dark", "stagingDir": "", "splitThreshold": 100,
		"bdmPrefixDefault": "OPL", "filesystemDefault": "fat32"}
	if code, _ := h.do("PUT", "/api/settings", put); code != http.StatusOK {
		t.Fatalf("put settings = %d", code)
	}
	if code := h.get("/api/settings", &settings); code != http.StatusOK ||
		settings["theme"] != "dark" {
		t.Fatalf("settings = %v", settings)
	}
	if code, _ := h.do("PUT", "/api/settings",
		map[string]any{"theme": "neon"}); code != http.StatusBadRequest {
		t.Fatalf("bad theme = %d, want 400", code)
	}
}

func TestIntegrationEnqueueValidation(t *testing.T) {
	h, cancel := newAPIHarness(t)
	defer cancel()
	srcDir := t.TempDir()
	buildFixtures(t, srcDir)
	destDir := t.TempDir()

	var dest destinationJSON
	if code, raw := h.do("POST", "/api/destinations",
		map[string]string{"path": destDir}); code != http.StatusCreated {
		t.Fatalf("create dest = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &dest); err != nil {
		t.Fatal(err)
	}
	// Unknown filesystem without override: 422 with guidance.
	var items []libraryItemJSON
	if code, raw := h.do("POST", "/api/library/import",
		map[string]string{"path": srcDir}); code != http.StatusCreated {
		t.Fatalf("import = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	var first []int64
	for _, it := range items {
		if it.Title == "small" {
			first = []int64{it.ID}
		}
	}
	if code, raw := h.do("POST", "/api/queue", map[string]any{
		"destinationId": dest.ID, "itemIds": first,
	}); code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown fs = %d, want 422\n%s", code, raw)
	}
	if code, _ := h.do("PATCH", fmt.Sprintf("/api/destinations/%d", dest.ID),
		map[string]string{"filesystemOverride": "fat32"}); code != http.StatusOK {
		t.Fatalf("override = %d", code)
	}
	// 5-disc group: N4 manifest rule fires before any write.
	var five []int64
	for _, it := range items {
		if it.Title == "Epic" {
			five = append(five, it.ID)
		}
	}
	if len(five) != 5 {
		t.Fatalf("want 5 Epic items, got %d", len(five))
	}
	if code, raw := h.do("POST", "/api/queue", map[string]any{
		"destinationId": dest.ID, "itemIds": five,
	}); code != http.StatusUnprocessableEntity {
		t.Fatalf("5-disc = %d, want 422\n%s", code, raw)
	} else if !strings.Contains(string(raw), "4") {
		t.Fatalf("422 lacks the disc-count constraint:\n%s", raw)
	}
	// Unknown item and empty list.
	if code, _ := h.do("POST", "/api/queue", map[string]any{
		"destinationId": dest.ID, "itemIds": []int64{9999},
	}); code != http.StatusNotFound {
		t.Fatalf("unknown item = %d, want 404", code)
	}
	if code, _ := h.do("POST", "/api/queue", map[string]any{
		"destinationId": dest.ID, "itemIds": []int64{},
	}); code != http.StatusBadRequest {
		t.Fatalf("empty items = %d, want 400", code)
	}
}

func TestIntegrationEnqueueSplitKind(t *testing.T) {
	h, cancel := newAPIHarness(t)
	defer cancel()
	// Sparse 5 GiB serial ISO: enqueue-only (paused) proves split
	// derivation through the API without writing gigabytes.
	srcDir := t.TempDir()
	_ = makeSparseSerialISO(t, srcDir)
	destDir := t.TempDir()
	var dest destinationJSON
	if code, raw := h.do("POST", "/api/destinations",
		map[string]string{"path": destDir}); code != http.StatusCreated {
		t.Fatalf("create dest = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &dest); err != nil {
		t.Fatal(err)
	}
	if code, _ := h.do("PATCH", fmt.Sprintf("/api/destinations/%d", dest.ID),
		map[string]string{"filesystemOverride": "fat32"}); code != http.StatusOK {
		t.Fatalf("override = %d", code)
	}
	var items []libraryItemJSON
	if code, raw := h.do("POST", "/api/library/import",
		map[string]string{"path": srcDir}); code != http.StatusCreated {
		t.Fatalf("import = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if code, _ := h.do("POST", "/api/queue/pause", nil); code != http.StatusOK {
		t.Fatalf("pause = %d", code)
	}
	var jobs []jobJSON
	if code, raw := h.do("POST", "/api/queue", map[string]any{
		"destinationId": dest.ID, "itemIds": []int64{items[0].ID},
	}); code != http.StatusCreated {
		t.Fatalf("enqueue = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Kind != "split-and-copy" || jobs[0].BytesTotal != 5<<30 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

// makeSparseSerialISO writes PVD+SYSTEM.CNF sectors then stretches sparse
// to 5 GiB: descriptor-valid, zero disk cost.
func makeSparseSerialISO(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "bigdvd.iso")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 20*2048)
	pvd := head[16*2048 : 17*2048]
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	copy(pvd[40:72], "BIGDVD")
	putU32 := func(b []byte, v uint32) {
		b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	}
	putU32(pvd[80:84], 3000000)
	copy(pvd[156:], []byte{34, 0})
	copy(pvd[156+2:], []byte{17, 0, 0, 0})
	copy(pvd[156+10:], []byte{0, 8, 0, 0})
	pvd[156+25] = 2
	pvd[156+28] = 1
	root := head[17*2048 : 18*2048]
	root[0], root[32] = 34, 1 // "." entry
	copy(root[2:6], []byte{17, 0, 0, 0})
	copy(root[10:14], []byte{0, 8, 0, 0})
	root[25] = 2
	off := 34
	rec := []byte("SYSTEM.CNF;1")
	root[off] = byte(33 + len(rec) + 1)
	copy(root[off+2:off+6], []byte{18, 0, 0, 0})
	cnf := "BOOT = cdrom:\\BIGG_001.01;1\n"
	copy(root[off+10:off+14], []byte{byte(len(cnf)), 0, 0, 0})
	root[off+32] = byte(len(rec))
	copy(root[off+33:], rec)
	copy(head[18*2048:], cnf)
	if _, err := f.Write(head); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(5 << 30); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIntegrationVolumes(t *testing.T) {
	h, cancel := newAPIHarness(t)
	defer cancel()
	// Contract: always 200 with an array of volume objects. Entries (if
	// any) must carry non-empty paths and known JSON keys.
	var vols []map[string]any
	if code := h.get("/api/destinations/volumes", &vols); code != http.StatusOK {
		t.Fatalf("volumes = %d", code)
	}
	if vols == nil {
		t.Fatal("volumes is null, want []")
	}
	for _, v := range vols {
		if v["path"] == "" {
			t.Errorf("volume with empty path: %v", v)
		}
		for _, k := range []string{"label", "filesystem", "freeBytes", "totalBytes", "removable", "added"} {
			if _, ok := v[k]; !ok {
				t.Errorf("volume lacks %s: %v", k, v)
			}
		}
		if v["added"] != false {
			t.Errorf("fresh db: volume should not be marked added: %v", v)
		}
	}
	// Tracking a listed volume as a destination flips its added flag.
	// Creating the destination row writes nothing to the mount itself
	// (DB row + read-only stat/probe), so this is safe on real volumes.
	// Skipped on machines with no detected volumes.
	if len(vols) > 0 {
		target := vols[0]["path"].(string)
		var dest destinationJSON
		if code, raw := h.do("POST", "/api/destinations",
			map[string]string{"path": target, "kind": "drive"}); code != http.StatusCreated {
			t.Fatalf("create dest = %d\n%s", code, raw)
		} else if err := json.Unmarshal(raw, &dest); err != nil {
			t.Fatal(err)
		}
		if code := h.get("/api/destinations/volumes", &vols); code != http.StatusOK {
			t.Fatalf("volumes = %d", code)
		}
		for _, v := range vols {
			if v["path"] == target && v["added"] != true {
				t.Errorf("tracked volume not marked added: %v", v)
			}
		}
	}
}

func TestIntegrationDestinationsReachable(t *testing.T) {
	h, cancel := newAPIHarness(t)
	defer cancel()
	live := t.TempDir()
	var created destinationJSON
	if code, raw := h.do("POST", "/api/destinations",
		map[string]string{"path": live}); code != http.StatusCreated {
		t.Fatalf("create dest = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	// Bypass create-time validation to track a dead (unplugged) path.
	dead := filepath.Join(t.TempDir(), "unplugged")
	if _, err := h.qstore().AddDestination(queue.Destination{Path: dead, Kind: queue.DestDrive}); err != nil {
		t.Fatal(err)
	}
	var dests []destinationJSON
	if code := h.get("/api/destinations", &dests); code != http.StatusOK {
		t.Fatalf("list = %d", code)
	}
	byPath := map[string]destinationJSON{}
	for _, d := range dests {
		byPath[d.Path] = d
	}
	if got, ok := byPath[live]; !ok || !got.Reachable {
		t.Errorf("live dest reachable = %+v, want true", got)
	}
	if got, ok := byPath[dead]; !ok || got.Reachable {
		t.Errorf("dead dest reachable = %+v, want false", got)
	}
	// DELETE removes an unused destination…
	if code, _ := h.do("DELETE", fmt.Sprintf("/api/destinations/%d", created.ID), nil); code != http.StatusOK {
		t.Fatalf("delete = %d", code)
	}
	if code := h.get("/api/destinations", &dests); code != http.StatusOK {
		t.Fatalf("list = %d", code)
	}
	for _, d := range dests {
		if d.Path == live {
			t.Fatalf("deleted destination survives: %+v", d)
		}
	}
	// …404s on unknown ids…
	if code, _ := h.do("DELETE", "/api/destinations/9999", nil); code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", code)
	}
	// …and 409s while jobs reference the destination.
	var withJobs destinationJSON
	if code, raw := h.do("POST", "/api/destinations",
		map[string]string{"path": live}); code != http.StatusCreated {
		t.Fatalf("create dest = %d\n%s", code, raw)
	} else if err := json.Unmarshal(raw, &withJobs); err != nil {
		t.Fatal(err)
	}
	jobs, err := h.qstore().Enqueue([]queue.Job{{LibraryItemID: 1, DestinationID: withJobs.ID, Kind: queue.KindCopy}})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := h.do("DELETE", fmt.Sprintf("/api/destinations/%d", withJobs.ID), nil); code != http.StatusConflict {
		t.Fatalf("delete with jobs = %d, want 409", code)
	}
	for _, j := range jobs {
		if err := h.qstore().CancelJob(j.ID); err != nil {
			t.Fatal(err)
		}
	}
	if code, _ := h.do("DELETE", fmt.Sprintf("/api/destinations/%d", withJobs.ID), nil); code != http.StatusOK {
		t.Fatalf("delete after cancel = %d, want 200", code)
	}
}

func TestIntegrationSSE(t *testing.T) {
	h, cancel := newAPIHarness(t)
	defer cancel()
	srcDir := t.TempDir()
	buildFixtures(t, srcDir)
	destDir := t.TempDir()
	var dest destinationJSON
	if code, raw := h.do("POST", "/api/destinations",
		map[string]string{"path": destDir}); code != http.StatusCreated {
		t.Fatalf("create dest = %d\n%s", code, raw)
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
	// Pause first: the job stays pending, so connect-time snapshot emits it.
	if code, _ := h.do("POST", "/api/queue/pause", nil); code != http.StatusOK {
		t.Fatalf("pause = %d", code)
	}
	var isoID int64
	for _, it := range items {
		if it.Title == "small" {
			isoID = it.ID
		}
	}
	if code, _ := h.do("POST", "/api/queue", map[string]any{
		"destinationId": dest.ID, "itemIds": []int64{isoID},
	}); code != http.StatusCreated {
		t.Fatalf("enqueue = %d", code)
	}
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	req, err := http.NewRequestWithContext(ctx, "GET", h.base+"/api/queue/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("bad event: %v", err)
		}
		if _, ok := ev["jobId"]; !ok {
			t.Fatalf("event lacks jobId: %v", ev)
		}
		for _, k := range []string{"phase", "bytesDone", "bytesTotal", "etaSec", "message"} {
			if _, ok := ev[k]; !ok {
				t.Fatalf("event lacks %s: %v", k, ev)
			}
		}
		return // first well-formed event proves the stream
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("stream: %v", err)
	}
	t.Fatal("no data events received")
}
