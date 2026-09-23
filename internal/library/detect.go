package library

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jo/TinyPS2Manager/internal/cuebin"
	"github.com/jo/TinyPS2Manager/internal/isotool"
)

//go:embed data/ps2_cd_titles.json
var bundledDB []byte

// DBEntry is one known title: the disc bucket plus the canonical name.
type DBEntry struct {
	DiscType DiscType `json:"discType"`
	Title    string   `json:"title"`
}

// TitleDB is the bundled known-games database (spec §2.3 #4, §10 Q3),
// optionally extended by a user file. It ships empty until curated entries
// land; the user file is the live extension point.
type TitleDB struct {
	entries map[string]DBEntry
}

type dbFile struct {
	Version int              `json:"_version"`
	Titles  map[string]DBEntry `json:"titles"`
}

// LoadBundled parses the embedded database.
func LoadBundled() (*TitleDB, error) {
	var f dbFile
	if err := json.Unmarshal(bundledDB, &f); err != nil {
		return nil, fmt.Errorf("parse bundled title DB: %w", err)
	}
	if f.Titles == nil {
		f.Titles = map[string]DBEntry{}
	}
	return &TitleDB{entries: f.Titles}, nil
}

// MergeUser overlays a user JSON file (same envelope) onto the database. A
// missing file is not an error — the bundled data simply stands alone.
func (db *TitleDB) MergeUser(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f dbFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("parse user title DB %s: %w", path, err)
	}
	for serial, e := range f.Titles {
		if e.DiscType != DiscCD && e.DiscType != DiscDVD {
			return fmt.Errorf("user title DB %s: %s has bad discType %q", path, serial, e.DiscType)
		}
		db.entries[serial] = e
	}
	return nil
}

// DefaultUserDBPath is the user-extensible sibling file.
func DefaultUserDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "oplbm", "ps2_cd_titles.user.json"), nil
}

// Lookup returns the entry for a serial, if known.
func (db *TitleDB) Lookup(serial string) (DBEntry, bool) {
	e, ok := db.entries[serial]
	return e, ok
}

// Detect applies the §2.3 strategy to a PS2 item and returns the winning
// disc type and method. PS1 items return empty values (discType is null per
// spec §8). Overrides are read from the store by content hash.
func Detect(item *LibraryItem, st *Store, db *TitleDB) (DiscType, DetectionMethod, error) {
	if item.Platform == PlatformPS1 {
		return "", "", nil
	}
	if ov, ok, err := st.GetOverride(item.ContentHash); err != nil {
		return "", "", err
	} else if ok {
		return ov, MethodOverride, nil
	}
	info, inspectErr := isotool.Inspect(item.SourcePath)
	var dbType DiscType
	var dbFound bool
	if db != nil {
		if serial, err := serialFor(item.SourcePath, item.SizeBytes); err == nil {
			if e, ok := db.Lookup(serial); ok {
				dbType, dbFound = e.DiscType, true
			}
		}
	}
	var infoPtr *isotool.ISOInfo
	if inspectErr == nil {
		infoPtr = &info
	}
	dt, m := decide(inspectErr, infoPtr, item.SizeBytes, dbType, dbFound)
	return dt, m, nil
}

// decide is the pure §2.3 priority kernel, unit-testable without files:
// override is handled by Detect; inspected evidence (UDF, or descriptor data
// size vs the CD capacity) decides when conclusive; otherwise the file-size
// heuristic runs; the database overrides a heuristic it disagrees with —
// and only a heuristic. Descriptor evidence always outranks the database:
// a PVD-measured size is ground truth about this image, while a DB row may
// describe a different regional build of the same serial.
func decide(inspectErr error, info *isotool.ISOInfo, sizeBytes int64, dbType DiscType, dbFound bool) (DiscType, DetectionMethod) {
	if inspectErr == nil && info != nil {
		if info.HasUDF {
			return DiscDVD, MethodInspected
		}
		if info.DataSizeBytes <= CDCapacityBytes {
			return DiscCD, MethodInspected
		}
		return DiscDVD, MethodInspected
	}
	var t DiscType
	var m DetectionMethod
	if sizeBytes <= CDCapacityBytes {
		t, m = DiscCD, MethodHeuristic
	} else {
		t, m = DiscDVD, MethodHeuristic
	}
	if dbFound && dbType != t {
		return dbType, MethodDatabase
	}
	return t, m
}

// serialFor extracts the disc serial from an ISO9660 image for DB lookup.
// Failures are silent by design: no serial just means the database step
// abstains.
func serialFor(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return cuebin.ExtractSerial(f, size)
}
