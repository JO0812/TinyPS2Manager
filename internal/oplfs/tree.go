package oplfs

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Bucket is an OPL device folder (spec §2.1, case-sensitive). The full v0.2
// set is listed so later milestones slot in without rework; M1 populates
// DVD/CD/POPS plus device-root ul.* sets.
type Bucket string

const (
	BucketDVD   Bucket = "DVD"
	BucketCD    Bucket = "CD"
	BucketPOPS  Bucket = "POPS"
	BucketEmber Bucket = "EMBER"
	BucketCHT   Bucket = "CHT"
	BucketART   Bucket = "ART"
	BucketAPPS  Bucket = "APPS"
	BucketCFG   Bucket = "CFG"
	BucketVMC   Bucket = "VMC"
	BucketTHM   Bucket = "THM"
	BucketLNG   Bucket = "LNG"
)

// validBuckets is the closed set of creatable folders.
var validBuckets = map[Bucket]bool{
	BucketDVD: true, BucketCD: true, BucketPOPS: true, BucketEmber: true,
	BucketCHT: true, BucketART: true, BucketAPPS: true, BucketCFG: true,
	BucketVMC: true, BucketTHM: true, BucketLNG: true,
}

// PlannedFile is one file to create. An empty Bucket means the device root,
// reserved for USBExtreme sets (ul.cfg + ul.*) per the §12 correction —
// never for game images.
type PlannedFile struct {
	Bucket Bucket
	Subdir string // single path element, "" for none
	Name   string
}

// TreePlan is the validated folder tree for one destination.
type TreePlan struct {
	Root      string // destination root
	BDMprefix string // optional subfolder ("" = drive root)
	Files     []PlannedFile
	// ExtraDirs are additional directories to create, relative to the
	// base (e.g. the shared POPS/<vmcdir> VMC folder when it owns no
	// files of its own).
	ExtraDirs []string
}

// Add appends a file to the plan (validated at Paths time).
func (p *TreePlan) Add(bucket Bucket, subdir, name string) {
	p.Files = append(p.Files, PlannedFile{Bucket: bucket, Subdir: subdir, Name: name})
}

// base joins root + prefix.
func (p *TreePlan) base() string {
	if p.BDMprefix == "" {
		return p.Root
	}
	return filepath.Join(p.Root, p.BDMprefix)
}

// Paths validates the whole plan (N4-style: before any write) and returns
// the directories to create (parents first, unique, sorted) and the files
// to write (sorted).
func (p *TreePlan) Paths() (dirs, files []string, err error) {
	if err := checkPrefix(p.BDMprefix); err != nil {
		return nil, nil, err
	}
	base := p.base()
	dirSet := map[string]bool{}
	var fileList []string
	hasVCD := false
	for _, f := range p.Files {
		// Exact uppercase suffix: our converter emits .VCD and POPSTARTER
		// keys per-game config off the canonical form.
		if strings.HasSuffix(f.Name, ".VCD") {
			hasVCD = true
		}
	}
	for _, f := range p.Files {
		if f.Bucket == "" {
			if f.Subdir != "" {
				return nil, nil, fmt.Errorf("root file %q must not have a subdir", f.Name)
			}
			if err := checkName(f.Name); err != nil {
				return nil, nil, err
			}
			if f.Name != "ul.cfg" && !strings.HasPrefix(f.Name, "ul.") {
				return nil, nil, fmt.Errorf("device root holds only ul.cfg/ul.* sets, not %q", f.Name)
			}
			fileList = append(fileList, filepath.Join(base, f.Name))
			continue
		}
		if !validBuckets[f.Bucket] {
			return nil, nil, fmt.Errorf("unknown bucket %q (want exact DVD, CD, POPS, …)", f.Bucket)
		}
		if f.Bucket == BucketPOPS && !hasVCD {
			return nil, nil, fmt.Errorf("refusing to create POPS/ with no VCD in the set (OPL never creates it)")
		}
		if err := checkElement(f.Subdir, true); err != nil {
			return nil, nil, fmt.Errorf("bad subdir: %w", err)
		}
		if err := checkName(f.Name); err != nil {
			return nil, nil, err
		}
		dir := filepath.Join(base, string(f.Bucket))
		if f.Subdir != "" {
			dir = filepath.Join(dir, f.Subdir)
		}
		dirSet[dir] = true
		// Parent bucket dir is implied by MkdirAll, but list it explicitly
		// so callers see the full contract.
		dirSet[filepath.Join(base, string(f.Bucket))] = true
		fileList = append(fileList, filepath.Join(dir, f.Name))
	}
	for _, extra := range p.ExtraDirs {
		if err := checkPrefix(extra); err != nil {
			return nil, nil, fmt.Errorf("extra dir: %w", err)
		}
		dirSet[filepath.Join(base, extra)] = true
	}
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	sort.Strings(fileList)
	return dirs, fileList, nil
}

// ValidateBDMPrefix checks a user-supplied BDM prefix (also used by the
// API destination endpoints).
func ValidateBDMPrefix(prefix string) error { return checkPrefix(prefix) }

// checkPrefix validates the BDM prefix: relative, no "..", no absolutes.
func checkPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if filepath.IsAbs(prefix) {
		return fmt.Errorf("BDM prefix must be relative, got %q", prefix)
	}
	for _, el := range strings.Split(filepath.ToSlash(prefix), "/") {
		if err := checkElement(el, false); err != nil {
			return fmt.Errorf("BDM prefix: %w", err)
		}
	}
	return nil
}

// checkElement validates one path element; emptyAllowed permits "" (no subdir).
func checkElement(el string, emptyAllowed bool) error {
	if el == "" {
		if emptyAllowed {
			return nil
		}
		return fmt.Errorf("empty path element")
	}
	if el == "." || el == ".." {
		return fmt.Errorf("reserved element %q", el)
	}
	if strings.ContainsAny(el, `/\`) {
		return fmt.Errorf("element %q contains a separator", el)
	}
	return nil
}

// checkName validates a file name: single element, non-empty.
func checkName(name string) error {
	if name == "" {
		return fmt.Errorf("empty file name")
	}
	return checkElement(name, false)
}
