package library

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/cuebin"
)

// discSuffix strips multi-disc markers (" (Disc 1)", " - Disk 2", "_CD3")
// for titles and group keys. Case-insensitive; anchored at the end so titles
// like "CD Player" (no trailing number) are untouched.
var discSuffix = regexp.MustCompile(`(?i)[\s_\-]+[\(\[]?(disc|disk|cd)\s*0*(\d+)[\)\]]?\s*$`)

// splitDiscSuffix returns the base title and disc number (0 when single).
func splitDiscSuffix(base string) (string, int) {
	m := discSuffix.FindStringSubmatch(base)
	if m == nil {
		return base, 0
	}
	n, _ := strconv.Atoi(m[2])
	stripped := strings.TrimRight(base[:len(base)-len(m[0])], " .")
	if stripped == "" {
		return base, 0
	}
	return stripped, n
}

// ScanDir walks root and emits deduped library items: `.iso` files become
// PS2 candidates; `.cue` files become PS1 items with their referenced BINs
// resolved (missing BINs mark the item error). Lone `.bin` files are skipped.
// No database is touched; callers persist via Store.
func ScanDir(root string) ([]LibraryItem, error) {
	var isos, cues []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".iso":
			isos = append(isos, path)
		case ".cue":
			cues = append(cues, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var items []LibraryItem
	seen := map[string]bool{} // content hash -> deduped
	add := func(it LibraryItem) {
		if seen[it.ContentHash] {
			return
		}
		seen[it.ContentHash] = true
		items = append(items, it)
	}

	for _, path := range isos {
		hash, size, err := ContentHash(path)
		if err != nil {
			continue // unreadable source: skip, don't poison the scan
		}
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		title, index := splitDiscSuffix(base)
		// PS2 multi-disc grouping lands in M4; the index is recorded now.
		add(LibraryItem{
			SourcePath: path, ContentHash: hash, Platform: PlatformPS2,
			Title: title, DiscIndex: index, SizeBytes: size, Status: StatusNew,
		})
	}

	type cueItem struct {
		item LibraryItem
		key  string // group key: dir + lowered base title
	}
	var cueItems []cueItem
	for _, path := range cues {
		it, key := scanCue(path)
		cueItems = append(cueItems, cueItem{it, key})
	}
	groups := map[string][]int{}
	for i, ci := range cueItems {
		groups[ci.key] = append(groups[ci.key], i)
	}
	var tmpGroup int64
	for _, idxs := range groups {
		if len(idxs) < 2 {
			add(cueItems[idxs[0]].item)
			continue
		}
		// Multi-disc set: real group IDs come from Store.NextGroupID at
		// persist time; mark membership with a temporary negative ID so
		// callers can tell grouped items apart before persisting.
		tmpGroup--
		for _, i := range idxs {
			it := cueItems[i].item
			g := tmpGroup
			it.DiscGroupID = &g
			add(it)
		}
	}
	return items, nil
}

// scanCue builds the PS1 item for one .cue file, verifying its BINs exist.
func scanCue(path string) (LibraryItem, string) {
	it := LibraryItem{
		SourcePath: path, Platform: PlatformPS1, Status: StatusNew,
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title, index := splitDiscSuffix(base)
	it.Title, it.DiscIndex = title, index
	key := filepath.Dir(path) + "\x00" + strings.ToLower(title)

	raw, err := os.ReadFile(path)
	if err != nil {
		it.Status = StatusError
		it.ContentHash = "unreadable:" + path
		return it, key
	}
	sheet, err := cuebin.Parse(string(raw))
	if err != nil {
		it.Status = StatusError
		it.ContentHash = fmt.Sprintf("badcue:%x", sha256.Sum256(raw))
		return it, key
	}
	dir := filepath.Dir(path)
	var binNames []string
	var binSizes []int64
	for _, tr := range sheet.Tracks {
		dup := false
		for _, n := range binNames {
			if n == tr.File {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		fi, err := os.Stat(filepath.Join(dir, tr.File))
		if err != nil {
			it.Status = StatusError
			it.ContentHash = fmt.Sprintf("missingbin:%x", sha256.Sum256(raw))
			return it, key
		}
		binNames = append(binNames, tr.File)
		binSizes = append(binSizes, fi.Size())
	}
	it.ContentHash = ps1Hash(raw, dir, sheet)
	it.SizeBytes = sumSizes(binSizes)
	return it, key
}

// ps1Hash keys a PS1 title by cue bytes plus each BIN's name, size, and
// first megabyte: re-rips of the same game keep the key (good for the
// override cache), while different games essentially never collide.
func ps1Hash(cue []byte, dir string, sheet *cuebin.Sheet) string {
	h := sha256.New()
	h.Write(cue)
	seen := map[string]bool{}
	for _, tr := range sheet.Tracks {
		if seen[tr.File] {
			continue
		}
		seen[tr.File] = true
		binPath := filepath.Join(dir, tr.File)
		var size int64
		if fi, err := os.Stat(binPath); err == nil {
			size = fi.Size()
		}
		fmt.Fprintf(h, "\x00%s:%d\x00", tr.File, size)
		if f, err := os.Open(binPath); err == nil {
			io.CopyN(h, f, hashPrefixBytes)
			f.Close()
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func sumSizes(s []int64) int64 {
	var total int64
	for _, v := range s {
		total += v
	}
	return total
}
