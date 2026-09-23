package cuebin

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Copy buffer size for the BIN→VCD stream (plan §3.3: 2 MiB).
const copyBufferSize = 2 << 20

// OpKind distinguishes the two merge operations.
type OpKind int

const (
	// OpCopy streams a byte range from a BIN file.
	OpCopy OpKind = iota
	// OpZero emits zero-filled sectors (pregap/postgap silence).
	OpZero
)

// Op is a single merge step in output order.
type Op struct {
	Kind OpKind
	// OpCopy fields:
	File   string // BIN filename as named in the sheet
	Offset int64  // byte offset within the BIN
	Length int64  // bytes to copy
	// OpZero fields:
	Sectors int64 // 2352-byte zero sectors to emit
}

// Plan is the ordered merge program turning a CUE sheet into one VCD.
type Plan struct {
	Ops        []Op
	TotalBytes int64
}

// BuildPlan compiles a parsed sheet into a merge Plan.
//
// binSizes maps sheet FILE names to their on-disk BIN sizes in bytes; every
// referenced BIN must be present. INDEX timestamps are interpreted as
// file-relative frame counts (the standard multi-file CUE convention; the
// single-file case is the degenerate instance). Validation is fail-closed:
// non-BINARY encodings, non-2352 sector modes, missing INDEX 01, inverted
// pregaps, and ragged BIN sizes are errors, never guesses.
func BuildPlan(s *Sheet, binSizes map[string]int64) (*Plan, error) {
	p := &Plan{}
	for i := range s.Tracks {
		tr := &s.Tracks[i]
		if strings.ToUpper(tr.FileType) != "BINARY" {
			return nil, fmt.Errorf("track %d: FILE type %q is not BINARY",
				tr.Number, tr.FileType)
		}
		if !tr.Mode.VCDNative() {
			return nil, fmt.Errorf("track %d: mode %s stores %d-byte sectors, "+
				"cannot merge verbatim into a 2352-byte/sector VCD",
				tr.Number, tr.Mode, tr.Mode.SectorSize())
		}
		size, ok := binSizes[tr.File]
		if !ok {
			return nil, fmt.Errorf("track %d: BIN %q size unknown", tr.Number, tr.File)
		}
		if size%SectorSizeVCD != 0 {
			return nil, fmt.Errorf("BIN %q size %d is not a multiple of %d (corrupt BIN?)",
				tr.File, size, SectorSizeVCD)
		}
		start, ok := tr.IndexPos(1)
		if !ok {
			return nil, fmt.Errorf("track %d: missing INDEX 01", tr.Number)
		}
		startFrame := int64(start.Frames())

		// Pregap: INDEX 00 gap plus any PREGAP directive, emitted as zeros.
		var gapFrames int64
		if pos00, ok := tr.IndexPos(0); ok {
			gapFrames = startFrame - int64(pos00.Frames())
			if gapFrames < 0 {
				return nil, fmt.Errorf("track %d: INDEX 00 follows INDEX 01", tr.Number)
			}
		}
		if tr.HasPregap {
			gapFrames += int64(tr.Pregap.Frames())
		}
		if gapFrames > 0 {
			p.Ops = append(p.Ops, Op{Kind: OpZero, Sectors: gapFrames})
			p.TotalBytes += gapFrames * SectorSizeVCD
		}

		// Data end: next track's gap/data start within the same BIN,
		// else the BIN's end.
		endFrame := size / SectorSizeVCD
		if i+1 < len(s.Tracks) && s.Tracks[i+1].File == tr.File {
			endFrame = nextStartFrame(&s.Tracks[i+1])
		}
		if endFrame < startFrame {
			return nil, fmt.Errorf("track %d: data region inverted (start %d > end %d)",
				tr.Number, startFrame, endFrame)
		}
		if length := (endFrame - startFrame) * SectorSizeVCD; length > 0 {
			p.Ops = append(p.Ops, Op{
				Kind:   OpCopy,
				File:   tr.File,
				Offset: startFrame * SectorSizeVCD,
				Length: length,
			})
			p.TotalBytes += length
		}

		if tr.HasPostgap {
			pg := int64(tr.Postgap.Frames())
			p.Ops = append(p.Ops, Op{Kind: OpZero, Sectors: pg})
			p.TotalBytes += pg * SectorSizeVCD
		}
	}
	return p, nil
}

// nextStartFrame returns the earliest frame (INDEX 00 if present, else
// INDEX 01) of the next track, i.e. where the current track's data must stop.
// Callers guarantee the next track parsed cleanly; a missing INDEX 01 there
// is a sheet error surfaced when that track is planned.
func nextStartFrame(nxt *Track) int64 {
	if pos00, ok := nxt.IndexPos(0); ok {
		return int64(pos00.Frames())
	}
	if pos01, ok := nxt.IndexPos(1); ok {
		return int64(pos01.Frames())
	}
	return 0
}

// WriteVCD executes a Plan, resolving BIN names under binDir and streaming
// the merged image to out. It returns the bytes written; on error the caller
// owns the partial output (delete it — queue cancellation, spec §6.4 #8).
func WriteVCD(p *Plan, binDir string, out io.Writer) (int64, error) {
	buf := make([]byte, copyBufferSize)
	var written int64
	for _, op := range p.Ops {
		switch op.Kind {
		case OpZero:
			n, err := writeZeros(out, op.Sectors, buf)
			written += n
			if err != nil {
				return written, err
			}
		case OpCopy:
			n, err := copyRange(binDir, op, out, buf)
			written += n
			if err != nil {
				return written, err
			}
		default:
			return written, fmt.Errorf("unknown op kind %d", op.Kind)
		}
	}
	return written, nil
}

// resolveBIN joins a sheet FILE name under binDir and rejects absolute
// paths and directory traversal.
func resolveBIN(binDir, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("BIN %q is an absolute path", name)
	}
	joined := filepath.Join(binDir, name)
	rel, err := filepath.Rel(binDir, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("BIN %q escapes the source directory", name)
	}
	return joined, nil
}

func copyRange(binDir string, op Op, out io.Writer, buf []byte) (int64, error) {
	path, err := resolveBIN(binDir, op.File)
	if err != nil {
		return 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open BIN %q: %w", op.File, err)
	}
	defer f.Close()
	n, err := io.CopyBuffer(out, io.NewSectionReader(f, op.Offset, op.Length), buf)
	if err != nil {
		return n, fmt.Errorf("copy BIN %q range: %w", op.File, err)
	}
	if n != op.Length {
		return n, fmt.Errorf("BIN %q truncated: got %d of %d bytes", op.File, n, op.Length)
	}
	return n, nil
}

// writeZeros emits sectors 2352-byte zero sectors, tolerating short writes.
func writeZeros(out io.Writer, sectors int64, zeroBuf []byte) (int64, error) {
	var written int64
	remaining := sectors * SectorSizeVCD
	for remaining > 0 {
		chunk := int64(len(zeroBuf))
		if chunk > remaining {
			chunk = remaining
		}
		n, err := out.Write(zeroBuf[:chunk])
		written += int64(n)
		remaining -= int64(n)
		if err != nil {
			return written, fmt.Errorf("write gap: %w", err)
		}
		if n == 0 {
			return written, fmt.Errorf("write gap: zero-length write")
		}
	}
	return written, nil
}
