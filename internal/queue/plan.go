package queue

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/cuebin"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/oplfs"
	"github.com/jo/TinyPS2Manager/internal/usbextreme"
)

// Estimate derives a job's kind and byte total from its library item and
// destination WITHOUT writing anything. The enqueue path (M2-4) uses it for
// validation-first checks (N4, free space); the executor re-derives the
// same plan at run time, so estimation and execution cannot drift.
func Estimate(item *library.LibraryItem, dest *Destination, lib *library.Store) (JobKind, int64, error) {
	switch item.Platform {
	case library.PlatformPS1:
		return estimatePS1(item, dest, lib)
	case library.PlatformPS2:
		return estimatePS2(item, dest)
	default:
		return "", 0, fmt.Errorf("unknown platform %q", item.Platform)
	}
}

func estimatePS1(item *library.LibraryItem, dest *Destination, lib *library.Store) (JobKind, int64, error) {
	plan, err := planConvert(item, dest, lib)
	if err != nil {
		return "", 0, err
	}
	return KindConvertCopy, plan.total, nil
}

func estimatePS2(item *library.LibraryItem, dest *Destination) (JobKind, int64, error) {
	fi, err := os.Stat(item.SourcePath)
	if err != nil {
		return "", 0, err
	}
	if item.DiscType == "" {
		return "", 0, fmt.Errorf("item %d has no disc type: inspect and set CD/DVD first", item.ID)
	}
	if item.DiscType == library.DiscDVD && dest.EffectiveFilesystem() == "fat32" &&
		fi.Size() > 4294967295 {
		serial, err := serialOf(item.SourcePath)
		if err != nil {
			return "", 0, fmt.Errorf("DVD needs splitting but has no readable serial: %v", err)
		}
		_ = serial
		return KindSplitAndCopy, fi.Size(), nil
	}
	return KindCopy, fi.Size(), nil
}

// serialOf extracts the disc serial from an ISO9660 image (2048-stride).
func serialOf(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	return cuebin.ExtractSerial(f, fi.Size(), 2048)
}

// CopyPreview is the dry-run shape of a copy plan (prepare endpoint).
type CopyPreview struct {
	DestPath string
}

// PreviewCopy resolves copy destinations without writing.
func PreviewCopy(item *library.LibraryItem, dest *Destination) (*CopyPreview, error) {
	p, err := planCopy(item, dest)
	if err != nil {
		return nil, err
	}
	return &CopyPreview{DestPath: p.destPath}, nil
}

// ManifestPreview is one generated text file plus its absolute path.
type ManifestPreview struct {
	Dir  string
	Name string
	Path string
}

// ConvertPreview is the dry-run shape of a convert plan.
type ConvertPreview struct {
	VCDPath   string
	PopsDir   string
	Manifests []ManifestPreview
	VMCDirs   []string
}

// PreviewConvert resolves VCD name, manifests, and VMC dirs without writing.
func PreviewConvert(item *library.LibraryItem, dest *Destination, lib *library.Store) (*ConvertPreview, error) {
	p, err := planConvert(item, dest, lib)
	if err != nil {
		return nil, err
	}
	out := &ConvertPreview{
		VCDPath: filepath.Join(p.popsDir, p.vcdName),
		PopsDir: p.popsDir,
		VMCDirs: p.vmcDirs,
	}
	for _, m := range p.manifests {
		out.Manifests = append(out.Manifests, ManifestPreview{
			Dir: m.dir, Name: m.name, Path: filepath.Join(m.dir, m.name),
		})
	}
	return out, nil
}

// SplitPreview is the dry-run shape of a split plan.
type SplitPreview struct {
	Root   string
	ULCfg  string
	Chunks []string
}

// PreviewSplit resolves the ul.cfg path and chunk names without writing.
func PreviewSplit(item *library.LibraryItem, dest *Destination) (*SplitPreview, error) {
	p, err := planSplit(item, dest)
	if err != nil {
		return nil, err
	}
	out := &SplitPreview{Root: p.destDir, ULCfg: filepath.Join(p.destDir, "ul.cfg")}
	for i := 0; i < p.chunks; i++ {
		out.Chunks = append(out.Chunks, filepath.Join(p.destDir, usbextreme.ChunkName(p.oplName, p.serial, i)))
	}
	return out, nil
}

type copyPlan struct {
	srcPath  string
	destPath string // under base (dest root + prefix), bucket included
	total    int64
}

// planCopy resolves source and destination paths. PS2 images keep their
// source basename verbatim; the bucket comes from the detected disc type.
func planCopy(item *library.LibraryItem, dest *Destination) (*copyPlan, error) {
	fi, err := os.Stat(item.SourcePath)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(dest.Path, dest.BDMPrefix)
	var bucket oplfs.Bucket
	var name string
	if item.Platform == library.PlatformPS1 {
		bucket = oplfs.BucketPOPS
		name = filepath.Base(item.SourcePath)
	} else {
		switch item.DiscType {
		case library.DiscCD:
			bucket = oplfs.BucketCD
		case library.DiscDVD:
			bucket = oplfs.BucketDVD
		default:
			return nil, fmt.Errorf("item %d has no disc type: inspect and set CD/DVD first", item.ID)
		}
		name = filepath.Base(item.SourcePath)
	}
	if err := checkFileName(name); err != nil {
		return nil, err
	}
	return &copyPlan{
		srcPath:  item.SourcePath,
		destPath: filepath.Join(base, string(bucket), name),
		total:    fi.Size(),
	}, nil
}

func checkFileName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("bad file name %q", name)
	}
	return nil
}

// manifestFile is one small generated text file (DISCS.TXT/VMCDIR.TXT).
type manifestFile struct {
	dir     string // absolute directory
	name    string
	content string
}

// convertPlan is a fully computed PS1 conversion: merge program, VCD name,
// manifests, and VMC directories. Nothing is written yet.
type convertPlan struct {
	sheet     *cuebin.Sheet
	binDir    string
	binSizes  map[string]int64
	merge     *cuebin.Plan
	vcdName   string
	popsDir   string // absolute POPS directory
	total     int64
	manifests []manifestFile
	vmcDirs   []string // absolute directories to create
}

// planConvert computes the merge, the VCD name (serial when extractable),
// and — for grouped multi-disc items — the identical manifests for every
// disc folder plus the shared VMC dir. Siblings are looked up through lib;
// their VCD names come from the same deterministic rule, so enqueue-time
// validation and run-time generation agree byte-for-byte.
func planConvert(item *library.LibraryItem, dest *Destination, lib *library.Store) (*convertPlan, error) {
	if item.Platform != library.PlatformPS1 {
		return nil, fmt.Errorf("item %d is not PS1", item.ID)
	}
	raw, err := os.ReadFile(item.SourcePath)
	if err != nil {
		return nil, err
	}
	sheet, err := cuebin.Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", item.SourcePath, err)
	}
	binDir := filepath.Dir(item.SourcePath)
	sizes := map[string]int64{}
	for _, tr := range sheet.Tracks {
		if _, ok := sizes[tr.File]; ok {
			continue
		}
		fi, err := os.Stat(filepath.Join(binDir, tr.File))
		if err != nil {
			return nil, fmt.Errorf("BIN %s: %w", tr.File, err)
		}
		sizes[tr.File] = fi.Size()
	}
	merge, err := cuebin.BuildPlan(sheet, sizes)
	if err != nil {
		return nil, err
	}
	serial := serialFromMerge(binDir, merge)
	vcdName := cuebin.VCDFileName(serial, item.Title)
	popsDir := filepath.Join(dest.Path, dest.BDMPrefix, string(oplfs.BucketPOPS))
	p := &convertPlan{
		sheet: sheet, binDir: binDir, binSizes: sizes, merge: merge,
		vcdName: vcdName, popsDir: popsDir, total: merge.TotalBytes,
	}

	if item.DiscGroupID == nil {
		return p, nil
	}
	siblings, err := lib.ListByGroupID(*item.DiscGroupID)
	if err != nil {
		return nil, err
	}
	// Structural count check before touching any sibling files.
	if err := oplfs.CheckDiscCount(len(siblings)); err != nil {
		return nil, err
	}
	// Order by disc index; collect every disc's VCD name.
	ordered := append([]library.LibraryItem{}, siblings...)
	sortByDiscIndex(ordered)
	vcds := make([]string, 0, len(ordered))
	for _, sib := range ordered {
		name, err := siblingVCDName(&sib)
		if err != nil {
			return nil, err
		}
		vcds = append(vcds, name)
	}
	set := oplfs.MultiDiscSet{VCDs: vcds, VMCDir: vmcDirFor(ordered, vcds)}
	if err := set.Validate(); err != nil {
		return nil, fmt.Errorf("multi-disc validation: %w", err)
	}
	discs, err := oplfs.BuildDISCSTXT(vcds)
	if err != nil {
		return nil, err
	}
	vmc, err := oplfs.BuildVMCDIRTXT(set.VMCDir)
	if err != nil {
		return nil, err
	}
	seenDirs := map[string]bool{}
	for _, sib := range ordered {
		folder, _ := oplfs.DiscFolder(vcdNameOf(sib, vcds, ordered))
		dir := filepath.Join(popsDir, folder)
		p.manifests = append(p.manifests,
			manifestFile{dir: dir, name: "DISCS.TXT", content: discs},
			manifestFile{dir: dir, name: "VMCDIR.TXT", content: vmc},
		)
		if !seenDirs[dir] {
			seenDirs[dir] = true
			p.vmcDirs = append(p.vmcDirs, dir)
		}
	}
	shared := filepath.Join(popsDir, set.VMCDir)
	if !seenDirs[shared] {
		p.vmcDirs = append(p.vmcDirs, shared)
	}
	return p, nil
}

// sortByDiscIndex orders siblings by disc number, breaking ties by ID.
func sortByDiscIndex(items []library.LibraryItem) {
	sort.Slice(items, func(a, b int) bool {
		if items[a].DiscIndex != items[b].DiscIndex {
			return items[a].DiscIndex < items[b].DiscIndex
		}
		return items[a].ID < items[b].ID
	})
}

// vcdNameOf pairs an ordered sibling with its precomputed VCD name.
func vcdNameOf(sib library.LibraryItem, vcds []string, ordered []library.LibraryItem) string {
	for i := range ordered {
		if ordered[i].ID == sib.ID {
			return vcds[i]
		}
	}
	return ""
}

// siblingVCDName deterministically names a sibling's VCD without writing.
func siblingVCDName(sib *library.LibraryItem) (string, error) {
	raw, err := os.ReadFile(sib.SourcePath)
	if err != nil {
		return "", err
	}
	sheet, err := cuebin.Parse(string(raw))
	if err != nil {
		return "", err
	}
	binDir := filepath.Dir(sib.SourcePath)
	sizes := map[string]int64{}
	for _, tr := range sheet.Tracks {
		if _, ok := sizes[tr.File]; ok {
			continue
		}
		fi, err := os.Stat(filepath.Join(binDir, tr.File))
		if err != nil {
			return "", fmt.Errorf("BIN %s: %w", tr.File, err)
		}
		sizes[tr.File] = fi.Size()
	}
	merge, err := cuebin.BuildPlan(sheet, sizes)
	if err != nil {
		return "", err
	}
	return cuebin.VCDFileName(serialFromMerge(binDir, merge), sib.Title), nil
}

// vmcDirFor names the shared-save folder after disc 1's VCD stem (spec
// §2.5: VMCDIR names disc 1's VMC folder).
func vmcDirFor(ordered []library.LibraryItem, vcds []string) string {
	folder, err := oplfs.DiscFolder(vcds[0])
	if err != nil {
		return "VMC0"
	}
	return folder
}

// serialFromMerge extracts the serial from the first data region (2352
// stride); "" when unreadable — the title-only fallback.
func serialFromMerge(binDir string, plan *cuebin.Plan) string {
	for _, op := range plan.Ops {
		if op.Kind != cuebin.OpCopy {
			continue
		}
		f, err := os.Open(filepath.Join(binDir, op.File))
		if err != nil {
			return ""
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil || fi.Size() <= op.Offset {
			return ""
		}
		sr := io.NewSectionReader(f, op.Offset, fi.Size()-op.Offset)
		serial, err := cuebin.ExtractSerial(sr, fi.Size()-op.Offset, 2352)
		if err != nil {
			return ""
		}
		return serial
	}
	return ""
}

// splitPlan is a USBExtreme split at the device root.
type splitPlan struct {
	srcPath string
	size    int64
	oplName string
	serial  string
	chunks  int
	destDir string // device root + prefix: ul.cfg + ul.* land here
}

// planSplit computes chunk count and names. Titles over the 32-byte OPL
// name field fail closed (rename the title, don't silently re-identity
// the chunk set, whose CRC derives from it).
func planSplit(item *library.LibraryItem, dest *Destination) (*splitPlan, error) {
	fi, err := os.Stat(item.SourcePath)
	if err != nil {
		return nil, err
	}
	if len(item.Title) == 0 || len(item.Title) > 32 {
		return nil, fmt.Errorf("title %q must be 1-32 bytes for the OPL name field", item.Title)
	}
	serial, err := serialOf(item.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("DVD needs splitting but %s has no readable serial: %w", item.SourcePath, err)
	}
	const chunk = int64(1) << 30
	n := int((fi.Size() + chunk - 1) / chunk)
	if n < 1 {
		n = 1
	}
	if n > 255 {
		return nil, fmt.Errorf("%d chunks exceeds USBExtreme limit", n)
	}
	return &splitPlan{
		srcPath: item.SourcePath, size: fi.Size(),
		oplName: item.Title, serial: serial, chunks: n,
		destDir: filepath.Join(dest.Path, dest.BDMPrefix),
	}, nil
}
