package oplfs

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Multi-disc limits (spec §2.5): POPSTARTER's in-game swap supports up to 4
// discs; VCD filenames target ≤ 73 chars for POPSLoader's path buffer
// (89 is the wiki ceiling — the stricter bound is enforced); VMCDIR.TXT is
// one line of ≤ 103 bytes with no path separators.
const (
	maxDiscs          = 4
	minDiscs          = 2 // single discs need no manifest at all
	vcdNameLimitChars = 73
	vmcdirLimitBytes  = 103
)

// MultiDiscSet is one PS1 multi-disc game: VCD filenames in play order plus
// the shared-save VMC folder name (disc 1's folder).
type MultiDiscSet struct {
	VCDs   []string
	VMCDir string
}

// DiscFolder derives the per-disc VMC folder from a VCD filename
// ("Game (Disc 1).VCD" -> "Game (Disc 1)").
func DiscFolder(vcdName string) (string, error) {
	if !strings.HasSuffix(vcdName, ".VCD") {
		return "", fmt.Errorf("VCD name %q lacks the .VCD suffix", vcdName)
	}
	stem := strings.TrimSuffix(vcdName, ".VCD")
	if err := checkElement(stem, false); err != nil {
		return "", fmt.Errorf("bad disc folder: %w", err)
	}
	return stem, nil
}

// validateVCDs enforces the per-entry manifest rules shared by Validate
// and BuildDISCSTXT.
func validateVCDs(vcds []string) error {
	if len(vcds) < minDiscs {
		return fmt.Errorf("DISCS.TXT needs %d-%d discs, got %d (singles need no manifest)", minDiscs, maxDiscs, len(vcds))
	}
	if len(vcds) > maxDiscs {
		return fmt.Errorf("POPSTARTER supports %d discs, got %d: split into manual reinstall waves", maxDiscs, len(vcds))
	}
	seen := map[string]bool{}
	for _, v := range vcds {
		if !strings.HasSuffix(v, ".VCD") {
			return fmt.Errorf("entry %q lacks the .VCD suffix", v)
		}
		if n := utf8.RuneCountInString(v); n > vcdNameLimitChars {
			return fmt.Errorf("entry %q is %d chars, limit %d", v, n, vcdNameLimitChars)
		}
		if seen[v] {
			return fmt.Errorf("duplicate entry %q", v)
		}
		seen[v] = true
		if _, err := DiscFolder(v); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the whole set before anything is generated (N4).
func (s MultiDiscSet) Validate() error {
	if err := validateVCDs(s.VCDs); err != nil {
		return err
	}
	return validateVMCDir(s.VMCDir)
}

// validateVMCDir enforces the VMCDIR.TXT content rule: single line,
// ≤ 103 bytes, no "/ \ :" characters, bare folder name.
func validateVMCDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("VMCDIR is empty")
	}
	if len(dir) > vmcdirLimitBytes {
		return fmt.Errorf("VMCDIR %q is %d bytes, limit %d", dir, len(dir), vmcdirLimitBytes)
	}
	if strings.ContainsAny(dir, `/\:`) {
		return fmt.Errorf("VMCDIR %q contains a forbidden character (/ \\ :)", dir)
	}
	if err := checkElement(dir, false); err != nil {
		return fmt.Errorf("VMCDIR: %w", err)
	}
	return nil
}

// BuildDISCSTXT renders the manifest: one .VCD filename per line, LF
// line endings, trailing newline. Identical content goes into every disc's
// folder (see Artifacts).
func BuildDISCSTXT(vcds []string) (string, error) {
	if err := validateVCDs(vcds); err != nil {
		return "", err
	}
	return strings.Join(vcds, "\n") + "\n", nil
}

// BuildVMCDIRTXT renders the single-line shared-save pointer.
func BuildVMCDIRTXT(vmcdir string) (string, error) {
	if err := validateVMCDir(vmcdir); err != nil {
		return "", err
	}
	return vmcdir + "\n", nil
}

// Artifacts expands a set into PlannedFiles (DISCS.TXT + VMCDIR.TXT inside
// every disc's folder) plus the directories to create (per-disc folders and
// the shared POPS/<vmcdir> VMC folder, created once).
func (s MultiDiscSet) Artifacts() (files []PlannedFile, dirs []string, err error) {
	if err := s.Validate(); err != nil {
		return nil, nil, err
	}
	for _, vcd := range s.VCDs {
		folder, _ := DiscFolder(vcd) // validated above
		files = append(files,
			PlannedFile{Bucket: BucketPOPS, Subdir: folder, Name: "DISCS.TXT"},
			PlannedFile{Bucket: BucketPOPS, Subdir: folder, Name: "VMCDIR.TXT"},
		)
		dirs = append(dirs, filepath.Join(string(BucketPOPS), folder))
	}
	dirs = append(dirs, filepath.Join(string(BucketPOPS), s.VMCDir))
	// Deduplicate: VMCDir conventionally equals disc 1's folder, and
	// creation is idempotent anyway — one entry per directory.
	seen := map[string]bool{}
	uniq := dirs[:0]
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			uniq = append(uniq, d)
		}
	}
	return files, uniq, nil
}
