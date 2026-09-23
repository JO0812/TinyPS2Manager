package cuebin

import (
	"fmt"
	"strconv"
	"strings"
)

// Raw sector size of a VCD image: 2352 bytes/sector (spec §2.4).
const SectorSizeVCD = 2352

// TrackMode is a CUE TRACK data mode.
type TrackMode string

const (
	TrackAudio      TrackMode = "AUDIO"
	TrackMode1_2048 TrackMode = "MODE1/2048"
	TrackMode1_2352 TrackMode = "MODE1/2352"
	TrackMode2_2336 TrackMode = "MODE2/2336"
	TrackMode2_2352 TrackMode = "MODE2/2352"
)

// SectorSize returns the on-disc sector size for a track mode, or -1 for an
// unknown mode.
func (m TrackMode) SectorSize() int {
	switch m {
	case TrackAudio, TrackMode1_2352, TrackMode2_2352:
		return 2352
	case TrackMode1_2048:
		return 2048
	case TrackMode2_2336:
		return 2336
	default:
		return -1
	}
}

// VCDNative reports whether the mode stores whole 2352-byte sectors that can
// be merged verbatim into a VCD.
func (m TrackMode) VCDNative() bool {
	return m.SectorSize() == SectorSizeVCD
}

// MSF is a CUE minute:second:frame timestamp. Frame rate is 75 fps.
type MSF struct {
	M, S, F int
}

// LBA converts the timestamp to a logical block address (frame count minus
// the 150-frame lead-in).
func (t MSF) LBA() int {
	return ((t.M*60)+t.S)*75 + t.F - 150
}

// Frames returns the raw frame count (no lead-in subtraction).
func (t MSF) Frames() int {
	return ((t.M*60)+t.S)*75 + t.F
}

// Index is a CUE INDEX entry: Number is the index number (0 = pregap,
// 1 = track start), Pos is its position within the track's FILE.
type Index struct {
	Number int
	Pos    MSF
}

// Track is a single CUE TRACK entry with its indices and gaps.
type Track struct {
	Number     int
	Mode       TrackMode
	File       string // BIN filename from the enclosing FILE statement
	FileType   string // file encoding from the FILE statement (must be BINARY to merge)
	Pregap     MSF    // PREGAP directive length (zero if absent)
	HasPregap  bool
	Postgap    MSF // POSTGAP directive length (zero if absent)
	HasPostgap bool
	Indices    []Index
}

// IndexPos returns the position of index Number, or false if absent.
func (t *Track) IndexPos(n int) (MSF, bool) {
	for _, ix := range t.Indices {
		if ix.Number == n {
			return ix.Pos, true
		}
	}
	return MSF{}, false
}

// Sheet is a parsed CUE sheet: an ordered list of tracks.
type Sheet struct {
	Tracks []Track
}

// ParseError is a CUE syntax error with a 1-based line number.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("cue line %d: %s", e.Line, e.Msg)
}

// Parse parses a CUE sheet. It accepts the full structural syntax (FILE,
// TRACK, INDEX, PREGAP, POSTGAP) and skips metadata (REM, FLAGS, ISRC,
// CATALOG, TITLE, PERFORMER, SONGWRITER, CDTEXTFILE). Capability checks
// (BINARY-only, 2352-native modes) are left to BuildPlan so syntax errors
// and unsupported-encoding errors stay distinct.
func Parse(input string) (*Sheet, error) {
	// Normalize CRLF (spec §2.9 convention applies to CUE/BIN rips too).
	input = strings.ReplaceAll(input, "\r\n", "\n")
	lines := strings.Split(input, "\n")

	s := &Sheet{}
	var curFile, curFileType string
	var cur *Track
	haveFile := false

	fail := func(line int, format string, args ...any) (*Sheet, error) {
		return nil, &ParseError{Line: line, Msg: fmt.Sprintf(format, args...)}
	}

	for i, raw := range lines {
		ln := i + 1
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		kw, rest := splitKeyword(line)
		switch kw {
		case "REM":
			continue // comment
		case "FILE":
			name, ftype, err := parseFile(rest)
			if err != nil {
				return fail(ln, "bad FILE statement: %v", err)
			}
			curFile, curFileType = name, ftype
			haveFile = true
			cur = nil
		case "TRACK":
			if !haveFile {
				return fail(ln, "TRACK before any FILE statement")
			}
			tr, err := parseTrack(rest, curFile, curFileType)
			if err != nil {
				return fail(ln, "bad TRACK statement: %v", err)
			}
			s.Tracks = append(s.Tracks, *tr)
			cur = &s.Tracks[len(s.Tracks)-1]
		case "INDEX":
			if cur == nil {
				return fail(ln, "INDEX outside any TRACK")
			}
			ix, err := parseIndex(rest)
			if err != nil {
				return fail(ln, "bad INDEX statement: %v", err)
			}
			cur.Indices = append(cur.Indices, *ix)
		case "PREGAP":
			if cur == nil {
				return fail(ln, "PREGAP outside any TRACK")
			}
			msf, err := parseMSF(rest)
			if err != nil {
				return fail(ln, "bad PREGAP timestamp: %v", err)
			}
			cur.Pregap, cur.HasPregap = msf, true
		case "POSTGAP":
			if cur == nil {
				return fail(ln, "POSTGAP outside any TRACK")
			}
			msf, err := parseMSF(rest)
			if err != nil {
				return fail(ln, "bad POSTGAP timestamp: %v", err)
			}
			cur.Postgap, cur.HasPostgap = msf, true
		case "FLAGS", "ISRC", "CATALOG", "CDTEXTFILE", "TITLE",
			"PERFORMER", "SONGWRITER":
			continue // metadata, irrelevant to the merge
		default:
			return fail(ln, "unknown directive %q", kw)
		}
	}

	if len(s.Tracks) == 0 {
		return nil, &ParseError{Line: 0, Msg: "sheet contains no TRACK entries"}
	}
	return s, nil
}

// splitKeyword splits "KEYWORD rest..." on the first run of whitespace.
func splitKeyword(line string) (kw, rest string) {
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		return strings.ToUpper(strings.TrimSpace(line[:i])), strings.TrimSpace(line[i+1:])
	}
	return strings.ToUpper(line), ""
}

// parseFile parses `"name" TYPE` (filename may be quoted or bare).
func parseFile(rest string) (name, ftype string, err error) {
	name, rest, err = splitQuoted(rest)
	if err != nil {
		return "", "", err
	}
	ftype, _ = splitKeyword(rest)
	if ftype == "" {
		return "", "", fmt.Errorf("missing file type")
	}
	return name, strings.ToUpper(ftype), nil
}

// splitQuoted splits a leading "quoted" or bare token from the rest.
func splitQuoted(s string) (tok, rest string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("missing token")
	}
	if s[0] == '"' {
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			return "", "", fmt.Errorf("unterminated quote")
		}
		return s[1 : 1+end], strings.TrimSpace(s[1+end+1:]), nil
	}
	kw, rest := splitKeyword(s)
	return kw, rest, nil
}

// parseTrack parses `NN MODE` into a Track bound to the current FILE.
func parseTrack(rest, file, fileType string) (*Track, error) {
	numStr, rest := splitKeyword(rest)
	modeStr, extra := splitKeyword(rest)
	if extra != "" {
		return nil, fmt.Errorf("trailing data after track mode")
	}
	num, err := strconv.Atoi(numStr)
	if err != nil || num < 1 || num > 99 {
		return nil, fmt.Errorf("bad track number %q", numStr)
	}
	mode := TrackMode(strings.ToUpper(modeStr))
	if mode.SectorSize() < 0 {
		return nil, fmt.Errorf("unknown track mode %q", modeStr)
	}
	return &Track{Number: num, Mode: mode, File: file, FileType: fileType}, nil
}

// parseIndex parses `NN MM:SS:FF`.
func parseIndex(rest string) (*Index, error) {
	numStr, rest := splitKeyword(rest)
	num, err := strconv.Atoi(numStr)
	if err != nil || num < 0 || num > 99 {
		return nil, fmt.Errorf("bad index number %q", numStr)
	}
	msfStr, extra := splitKeyword(rest)
	if extra != "" {
		return nil, fmt.Errorf("trailing data after index timestamp")
	}
	msf, err := parseMSF(msfStr)
	if err != nil {
		return nil, err
	}
	return &Index{Number: num, Pos: msf}, nil
}

// parseMSF parses `MM:SS:FF` with range checks (SS 0-59, FF 0-74).
func parseMSF(s string) (MSF, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return MSF{}, fmt.Errorf("bad timestamp %q, want MM:SS:FF", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return MSF{}, fmt.Errorf("bad timestamp %q", s)
		}
		nums[i] = n
	}
	if nums[1] > 59 {
		return MSF{}, fmt.Errorf("seconds out of range in %q", s)
	}
	if nums[2] > 74 {
		return MSF{}, fmt.Errorf("frames out of range in %q", s)
	}
	return MSF{M: nums[0], S: nums[1], F: nums[2]}, nil
}
