package cheats

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

// PS2RD file rules (spec §2.9):
// - One file per game, filename = <GameID>.cht (e.g. SLUS_213.85.cht)
// - PS2RD text format: name line followed by 16-hex-digit "address value"
//   lines; anything else shaped is name/comment (// or #; ; is NOT comment;
//   blank lines ignored; LF or CRLF).
// - Master Code is mandatory: 9-type hook line must head file.
// - Engine limits: ≤250 named cheats per file, types 8/A/B are parsed-but-skipped.
// - Never merge two masters: files are staged whole.

const (
	MaxCheatsPerFile = 250
)

// RawCheat is one named cheat from a source pack.
type RawCheat struct {
	Name  string
	Codes []string // each "XXXXXXXX YYYYYYYY" (8+space+8 hex)
}

// RawGame is one title's cheats from CheatDatabase.txt.
type RawGame struct {
	Title  string
	Cheats []RawCheat
}

var (
	hexCodeRe = regexp.MustCompile(`^[0-9A-Fa-f]{8}\s+[0-9A-Fa-f]{8}$`)
	nameRe    = regexp.MustCompile(`^[0-9A-Fa-f]{16}$`) // without space, also treat as code if 16 hex
)

// ParseDatabase parses CheatDevice CheatDatabase.txt (title-keyed sections).
// It returns map from game title as it appears in the file to its cheats.
// The caller maps via library Title/GameID and records regionMatched.
// Blank lines ignored; // or # comments ignored; ; is NOT a comment.
func ParseDatabase(data []byte) (map[string]RawGame, error) {
	// Normalize CRLF to LF
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// Trim and filter, but keep structure for lookahead
	type lineInfo struct {
		raw     string
		trimmed string
		isBlank bool
		isComm  bool
		isHex   bool
	}
	var infos []lineInfo
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		isBlank := trim == ""
		isComm := strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#")
		isHex := hexCodeRe.MatchString(trim) || nameRe.MatchString(trim)
		// normalize 16-char without space to spaced form
		if nameRe.MatchString(trim) {
			isHex = true
		}
		infos = append(infos, lineInfo{raw: l, trimmed: trim, isBlank: isBlank, isComm: isComm, isHex: isHex})
	}
	// Build list of meaningful lines (not blank, not comment) with original index
	var meaningful []int
	for i, inf := range infos {
		if inf.isBlank || inf.isComm {
			continue
		}
		meaningful = append(meaningful, i)
	}
	out := map[string]RawGame{}
	var curGame *RawGame
	var curCheat *RawCheat

	// Helper to peek next meaningful line's type
	peekIsHex := func(idxPos int) bool {
		if idxPos+1 >= len(meaningful) {
			return false
		}
		nextIdx := meaningful[idxPos+1]
		return infos[nextIdx].isHex
	}

	for pos := 0; pos < len(meaningful); {
		idx := meaningful[pos]
		inf := infos[idx]
		if inf.isHex {
			// Hex belongs to current cheat
			if curCheat == nil || curGame == nil {
				// Orphan hex, skip
				pos++
				continue
			}
			// Normalize to spaced uppercase
			code := strings.ToUpper(inf.trimmed)
			if len(code) == 16 && !strings.Contains(code, " ") {
				code = code[:8] + " " + code[8:]
			} else {
				// Ensure single space
				parts := strings.Fields(code)
				if len(parts) == 2 {
					code = parts[0] + " " + parts[1]
				}
			}
			curCheat.Codes = append(curCheat.Codes, code)
			pos++
			continue
		}
			// Name line - strip surrounding quotes for title/cheat names
		cleanName := strings.Trim(inf.trimmed, `"`)
		// Determine if it's a game title or cheat name via lookahead
		isCheat := peekIsHex(pos)
		if isCheat {
			// It's a cheat name under current game
			if curGame == nil {
				curGame = &RawGame{Title: cleanName}
			}
			// Finish previous cheat
			if curCheat != nil && len(curCheat.Codes) > 0 {
				curGame.Cheats = append(curGame.Cheats, *curCheat)
			}
			curCheat = &RawCheat{Name: cleanName}
			pos++
		} else {
			// It's a game title
			// Flush previous game+cheat
			if curGame != nil {
				if curCheat != nil && len(curCheat.Codes) > 0 {
					curGame.Cheats = append(curGame.Cheats, *curCheat)
					curCheat = nil
				}
				if len(curGame.Cheats) > 0 || curGame.Title != "" {
					out[curGame.Title] = *curGame
				}
			}
			curGame = &RawGame{Title: cleanName}
			curCheat = nil
			pos++
		}
	}
	// Flush tail
	if curGame != nil {
		if curCheat != nil && len(curCheat.Codes) > 0 {
			curGame.Cheats = append(curGame.Cheats, *curCheat)
		}
		if len(curGame.Cheats) > 0 {
			out[curGame.Title] = *curGame
		}
	}
	return out, nil
}

// Warnings from Build.
type Warnings struct {
	DroppedCount       int  // cheats dropped beyond 250
	HasEngineSkipped   bool // file relies on types 8/A/B
	HasMultipleMasters bool // more than one 9-type line (should be exactly 1)
}

// Build generates a single .cht file content for gameID from cheats.
// It validates exactly one master (9-type), limits to 250 cheats, and
// flags engine-skipped types. It never merges two masters.
func Build(gameID string, cheats []RawCheat) (string, Warnings, error) {
	if gameID == "" {
		return "", Warnings{}, fmt.Errorf("gameID is empty")
	}
	// Count masters (9-type) and engine-skipped (8/A/B)
	masterCount := 0
	hasSkipped := false
	for _, ch := range cheats {
		for _, code := range ch.Codes {
			if len(code) < 1 {
				continue
			}
			typ := strings.ToUpper(string(code[0]))
			if typ == "9" {
				masterCount++
			}
			if typ == "8" || typ == "A" || typ == "B" {
				hasSkipped = true
			}
		}
	}
	warns := Warnings{HasEngineSkipped: hasSkipped}
	if masterCount != 1 {
		if masterCount > 1 {
			warns.HasMultipleMasters = true
		}
		return "", warns, fmt.Errorf("no master code: need exactly one 9-type line, got %d", masterCount)
	}
	// Enforce ≤250; drop extras with warning
	if len(cheats) > MaxCheatsPerFile {
		warns.DroppedCount = len(cheats) - MaxCheatsPerFile
		cheats = cheats[:MaxCheatsPerFile]
	}
	var buf bytes.Buffer
	for i, ch := range cheats {
		if i > 0 {
			buf.WriteString("\n")
		}
		// Name line: if it looks like a comment, keep as is; otherwise write as name
		buf.WriteString(ch.Name)
		buf.WriteString("\n")
		for _, code := range ch.Codes {
			buf.WriteString(code)
			buf.WriteString("\n")
		}
	}
	return buf.String(), warns, nil
}

// ParseWidescreenDir reads already per-<GameID>.cht files from dir.
// It returns map GameID -> raw file bytes (used whole, not merged).
func ParseWidescreenDir(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".cht") {
			continue
		}
		gameID := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		// Validate gameID looks like serial (but allow any for now)
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out[gameID] = data
	}
	return out, nil
}

// ValidateCHT checks a built .cht content for spec compliance: exactly one
// master, ≤250 cheats. It returns warnings but error only on hard failure.
func ValidateCHT(content string) (Warnings, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var cheats []RawCheat
	var cur *RawCheat
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if trim == "" || strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#") {
			continue
		}
		if hexCodeRe.MatchString(trim) || regexp.MustCompile(`^[0-9A-Fa-f]{16}$`).MatchString(trim) {
			if cur != nil {
				cur.Codes = append(cur.Codes, trim)
			}
			continue
		}
		// Name line
		if cur != nil {
			cheats = append(cheats, *cur)
		}
		cur = &RawCheat{Name: trim}
	}
	if cur != nil {
		cheats = append(cheats, *cur)
	}
	_, warns, err := Build("TEST_000.00", cheats) // dummy ID for validation
	// Build will error on missing master; we want to surface that
	if err != nil {
		// But for Validate, we want to return the warnings even on error
		return warns, err
	}
	if len(cheats) > MaxCheatsPerFile {
		warns.DroppedCount = len(cheats) - MaxCheatsPerFile
	}
	return warns, nil
}

// Stage writes content to <root>/<prefix>/CHT/<GameID>.cht via disk.
// It validates before writing, respects gameIdUncertain gating, and
// never overwrites a hand file (highest trust). The caller picks the
// winner (gameplay OR widescreen OR hand) and calls Stage once.
func Stage(ctx context.Context, disk transfer.Disk, root, prefix string, item library.LibraryItem, content string, confirmed bool) error {
	if item.GameID == "" {
		return fmt.Errorf("item %d has no GameID: cannot stage cheats", item.ID)
	}
	if item.GameIDUncertain && !confirmed {
		return fmt.Errorf("item %d (%q) has uncertain GameID %q: explicit confirm required before staging cheats", item.ID, item.Title, item.GameID)
	}
	// Validate
	if _, warns, err := Build(item.GameID, parseCHTToRaw(content)); err != nil {
		return err
	} else if warns.HasMultipleMasters {
		// Treat as hard error already, but keep for completeness
		return fmt.Errorf("multiple masters")
	}
	dir := filepath.Join("CHT")
	if prefix != "" {
		dir = filepath.Join(prefix, dir)
	}
	absDir := filepath.Join(root, dir)
	if err := disk.MkdirAll(absDir); err != nil {
		return err
	}
	final := filepath.Join(root, dir, item.GameID+".cht")
	if _, err := disk.Stat(final); err == nil {
		// Hand file already present: never overwrite
		return nil
	}
	return disk.CopyToDest(ctx, final, strings.NewReader(content), int64(len(content)), nil)
}

func parseCHTToRaw(content string) []RawCheat {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var cheats []RawCheat
	var cur *RawCheat
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if trim == "" || strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#") {
			continue
		}
		if hexCodeRe.MatchString(trim) || regexp.MustCompile(`^[0-9A-Fa-f]{16}$`).MatchString(trim) {
			if cur != nil {
				cur.Codes = append(cur.Codes, trim)
			}
			continue
		}
		if cur != nil {
			cheats = append(cheats, *cur)
		}
		cur = &RawCheat{Name: trim}
	}
	if cur != nil {
		cheats = append(cheats, *cur)
	}
	return cheats
}
