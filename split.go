package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/cuebin"
	"github.com/jo/TinyPS2Manager/internal/usbextreme"
)

func cmdSplit(args []string) error {
	fs := flag.NewFlagSet("split", flag.ContinueOnError)
	outDir := fs.String("out", "", "output directory: ul.cfg + chunks land at its root (required)")
	name := fs.String("name", "", "OPL display name (default: ISO filename)")
	serial := fs.String("serial", "", "disc serial SXXX_NNN.NN (default: read from SYSTEM.CNF)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm split --out <dir> [--name N] [--serial S] <iso>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || *outDir == "" {
		fs.Usage()
		return fmt.Errorf("want one ISO path plus --out")
	}
	isoPath := fs.Arg(0)
	f, err := os.Open(isoPath)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	oplName := *name
	if oplName == "" {
		oplName = strings.TrimSuffix(filepath.Base(isoPath), filepath.Ext(isoPath))
	}
	s := *serial
	if s == "" {
		var serr error
		s, serr = cuebin.ExtractSerial(f, fi.Size(), 2048)
		if serr != nil {
			return fmt.Errorf("no --serial given and SYSTEM.CNF unreadable: %v", serr)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	n, err := usbextreme.Write(*outDir, oplName, s, f, fi.Size())
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d chunks + ul.cfg entry for %s [%s] in %s\n", n, oplName, s, *outDir)
	return nil
}
