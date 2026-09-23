package library

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/jo/TinyPS2Manager/internal/isotool"
)

func TestDecide(t *testing.T) {
	iso := func(udf bool, sectors uint32) *isotool.ISOInfo {
		return &isotool.ISOInfo{HasISO9660: true, HasUDF: udf,
			SectorCount: sectors, DataSizeBytes: int64(sectors) * 2048}
	}
	cases := map[string]struct {
		err  error
		info *isotool.ISOInfo
		size int64
		db   DiscType
		dbOK bool
		want DiscType
		m    DetectionMethod
	}{
		"udf wins":            {nil, iso(true, 100), 1 << 20, DiscCD, true, DiscDVD, MethodInspected},
		"small iso is cd":     {nil, iso(false, 1000), 1 << 20, "", false, DiscCD, MethodInspected},
		"big iso is dvd":      {nil, iso(false, 5_000_000), 9 << 30, "", false, DiscDVD, MethodInspected},
		"heuristic cd":        {errNope, nil, 100, "", false, DiscCD, MethodHeuristic},
		"heuristic dvd":       {errNope, nil, 5 << 30, "", false, DiscDVD, MethodHeuristic},
		"db agrees heuristic": {errNope, nil, 100, DiscCD, true, DiscCD, MethodHeuristic},
		"db beats heuristic":  {errNope, nil, 5 << 30, DiscCD, true, DiscCD, MethodDatabase},
		"db beats heuristic2": {errNope, nil, 100, DiscDVD, true, DiscDVD, MethodDatabase},
		"descriptors beat db": {nil, iso(false, 5_000_000), 9 << 30, DiscCD, true, DiscDVD, MethodInspected},
	}
	for name, tc := range cases {
		got, m := decide(tc.err, tc.info, tc.size, tc.db, tc.dbOK)
		if got != tc.want || m != tc.m {
			t.Errorf("%s = (%q,%q), want (%q,%q)", name, got, m, tc.want, tc.m)
		}
	}
}

var errNope = errorString("nope")

type errorString string

func (e errorString) Error() string { return string(e) }

// makeSerialISO builds a tiny ISO9660 image with a PVD claiming sectors
// sectors and a SYSTEM.CNF carrying serial.
func makeSerialISO(t *testing.T, sectors uint32, serial string) string {
	t.Helper()
	img := make([]byte, 16*2048)
	pvd := make([]byte, 2048)
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	copy(pvd[40:72], "TESTVOL")
	binary.LittleEndian.PutUint32(pvd[80:84], sectors)
	img = append(img, pvd...)
	root := appendRec(nil, 17, 2048, 2, []byte{0})
	root = appendRec(root, 17, 2048, 2, []byte{1})
	cnf := "BOOT = cdrom:\\" + serial + ";1\n"
	root = appendRec(root, 18, uint32(len(cnf)), 0, []byte("SYSTEM.CNF;1"))
	sec := make([]byte, 2048)
	copy(sec, root)
	img = append(img, sec...)
	dat := make([]byte, 2048)
	copy(dat, cnf)
	img = append(img, dat...)
	path := filepath.Join(t.TempDir(), "s.iso")
	if err := os.WriteFile(path, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendRec(buf []byte, extent, size uint32, flags byte, name []byte) []byte {
	recLen := 33 + len(name)
	if len(name)%2 == 0 {
		recLen++
	}
	rec := make([]byte, recLen)
	rec[0] = byte(recLen)
	binary.LittleEndian.PutUint32(rec[2:6], extent)
	binary.LittleEndian.PutUint32(rec[10:14], size)
	rec[25] = flags
	rec[32] = byte(len(name))
	copy(rec[33:], name)
	return append(buf, rec...)
}

func writeUserDB(t *testing.T, serial string, dt DiscType) string {
	t.Helper()
	content := `{"_version":1,"titles":{"` + serial + `":{"discType":"` +
		string(dt) + `","title":"T"}}}`
	path := filepath.Join(t.TempDir(), "user.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectDescriptorBeatsDB(t *testing.T) {
	st := openTestStore(t)
	db, err := LoadBundled()
	if err != nil {
		t.Fatalf("LoadBundled: %v", err)
	}
	// Small PVD (CD by inspection) vs user DB claiming DVD: descriptors win.
	if err := db.MergeUser(writeUserDB(t, "TEST_001.01", DiscDVD)); err != nil {
		t.Fatal(err)
	}
	path := makeSerialISO(t, 1000, "TEST_001.01")
	hash, size := mustHash(t, path)
	item := LibraryItem{SourcePath: path, ContentHash: hash,
		Platform: PlatformPS2, SizeBytes: size}
	if _, err := st.UpsertItem(item); err != nil {
		t.Fatal(err)
	}
	dt, m, err := Detect(&item, st, db)
	if err != nil {
		t.Fatal(err)
	}
	if dt != DiscCD || m != MethodInspected {
		t.Errorf("got (%q,%q), want (cd,inspected)", dt, m)
	}
}

func TestDetectOverrideFirst(t *testing.T) {
	st := openTestStore(t)
	path := makeSerialISO(t, 5_000_000, "TEST_001.01") // inspected DVD
	hash, size := mustHash(t, path)
	item := LibraryItem{SourcePath: path, ContentHash: hash,
		Platform: PlatformPS2, SizeBytes: size}
	if _, err := st.UpsertItem(item); err != nil {
		t.Fatal(err)
	}
	if err := st.SetDiscOverride(hash, DiscCD); err != nil {
		t.Fatal(err)
	}
	db, _ := LoadBundled()
	dt, m, err := Detect(&item, st, db)
	if err != nil {
		t.Fatal(err)
	}
	if dt != DiscCD || m != MethodOverride {
		t.Errorf("got (%q,%q), want (cd,override)", dt, m)
	}
}

func TestDetectPS1Empty(t *testing.T) {
	st := openTestStore(t)
	db, _ := LoadBundled()
	dt, m, err := Detect(&LibraryItem{Platform: PlatformPS1}, st, db)
	if err != nil || dt != "" || m != "" {
		t.Errorf("ps1 = (%q,%q,%v), want empties", dt, m, err)
	}
}

func TestDetectHeuristicOnGarbage(t *testing.T) {
	st := openTestStore(t)
	path := filepath.Join(t.TempDir(), "g.iso")
	if err := os.WriteFile(path, []byte("not an iso"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, size := mustHash(t, path)
	item := LibraryItem{SourcePath: path, ContentHash: hash,
		Platform: PlatformPS2, SizeBytes: size}
	if _, err := st.UpsertItem(item); err != nil {
		t.Fatal(err)
	}
	db, _ := LoadBundled()
	dt, m, err := Detect(&item, st, db)
	if err != nil {
		t.Fatal(err)
	}
	if dt != DiscCD || m != MethodHeuristic {
		t.Errorf("got (%q,%q), want (cd,heuristic)", dt, m)
	}
}

func TestTitleDBFiles(t *testing.T) {
	db, err := LoadBundled()
	if err != nil {
		t.Fatalf("LoadBundled: %v", err)
	}
	if _, ok := db.Lookup("NOPE_000.00"); ok {
		t.Error("empty bundle should miss")
	}
	if err := db.MergeUser(filepath.Join(t.TempDir(), "absent.json")); err != nil {
		t.Errorf("missing user DB should be nil: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"titles":{"X":{"discType":"bluray"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.MergeUser(bad); err == nil {
		t.Error("expected error for bad discType")
	}
	if err := db.MergeUser(writeUserDB(t, "TEST_001.01", DiscCD)); err != nil {
		t.Fatal(err)
	}
	e, ok := db.Lookup("TEST_001.01")
	if !ok || e.DiscType != DiscCD {
		t.Errorf("lookup = %+v,%v", e, ok)
	}
}

func mustHash(t *testing.T, path string) (string, int64) {
	t.Helper()
	h, size, err := ContentHash(path)
	if err != nil {
		t.Fatal(err)
	}
	return h, size
}
