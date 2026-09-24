package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/cuebin"
	"github.com/jo/TinyPS2Manager/internal/library"
)

func cmdConvert(args []string) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	dbPath := fs.String("db", "", "SQLite library path (default $CONFIG/oplbm/oplbm.db)")
	outDir := fs.String("out", "", "output directory for the .VCD (required)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm convert --out <dir> [--db path] <ps1-id|.cue>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *outDir == "" {
		fs.Usage()
		return fmt.Errorf("want one PS1 id or .cue path plus --out")
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	cuePath, title, err := resolveConvertSource(fs.Arg(0), *dbPath)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(cuePath)
	if err != nil {
		return err
	}
	sheet, err := cuebin.Parse(string(raw))
	if err != nil {
		return fmt.Errorf("parse %s: %w", cuePath, err)
	}
	binDir := filepath.Dir(cuePath)
	sizes := map[string]int64{}
	for _, tr := range sheet.Tracks {
		if _, ok := sizes[tr.File]; ok {
			continue
		}
		fi, err := os.Stat(filepath.Join(binDir, tr.File))
		if err != nil {
			return fmt.Errorf("BIN %s: %w", tr.File, err)
		}
		sizes[tr.File] = fi.Size()
	}
	plan, err := cuebin.BuildPlan(sheet, sizes)
	if err != nil {
		return err
	}
	serial := serialFromPlan(binDir, plan)
	name := cuebin.VCDFileName(serial, title)
	dest := filepath.Join(*outDir, name)
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	n, err := cuebin.WriteVCD(plan, binDir, f)
	cerr := f.Close()
	if err != nil {
		os.Remove(dest)
		return err
	}
	if cerr != nil {
		os.Remove(dest)
		return cerr
	}
	fmt.Printf("wrote %s (%d bytes)\n", dest, n)
	return nil
}

// resolveConvertSource maps an id or .cue path to the cue file and title.
func resolveConvertSource(arg, dbFlag string) (cuePath, title string, err error) {
	if strings.HasSuffix(strings.ToLower(arg), ".cue") {
		if _, err := os.Stat(arg); err != nil {
			return "", "", err
		}
		base := strings.TrimSuffix(filepath.Base(arg), filepath.Ext(arg))
		return arg, base, nil
	}
	id, perr := strconv.ParseInt(arg, 10, 64)
	if perr != nil {
		return "", "", fmt.Errorf("want a PS1 library id or a .cue path, got %q", arg)
	}
	db, err := resolveDB(dbFlag)
	if err != nil {
		return "", "", err
	}
	st, err := library.Open(db)
	if err != nil {
		return "", "", err
	}
	defer st.Close()
	it, err := st.Get(id)
	if err != nil {
		return "", "", err
	}
	if it == nil {
		return "", "", fmt.Errorf("no library item %d", id)
	}
	if it.Platform != library.PlatformPS1 {
		return "", "", fmt.Errorf("item %d is %s, not PS1", id, it.Platform)
	}
	if it.Status == library.StatusError {
		return "", "", fmt.Errorf("item %d has error status: fix the sources and re-import", id)
	}
	return it.SourcePath, it.Title, nil
}

// serialFromPlan extracts the disc serial from the first data region's track
// image (2352-stride). Any failure yields "" and the title-only filename.
func serialFromPlan(binDir string, plan *cuebin.Plan) string {
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
