package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/library"
)

func cmdInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	dbPath := fs.String("db", "", "SQLite library path (default $CONFIG/oplbm/oplbm.db)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm inspect [--db path] <id|path>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("want exactly one id or path")
	}
	db, err := resolveDB(*dbPath)
	if err != nil {
		return err
	}
	st, err := library.Open(db)
	if err != nil {
		return err
	}
	defer st.Close()

	titledb, err := library.LoadBundled()
	if err != nil {
		return err
	}
	if userPath, err := library.DefaultUserDBPath(); err == nil {
		if err := titledb.MergeUser(userPath); err != nil {
			return err
		}
	}

	arg := fs.Arg(0)
	if id, err := strconv.ParseInt(arg, 10, 64); err == nil {
		it, err := st.Get(id)
		if err != nil {
			return err
		}
		if it == nil {
			return fmt.Errorf("no library item %d", id)
		}
		printItem(it)
		return nil
	}
	// Path mode: detect without persisting.
	fi, err := os.Stat(arg)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(arg))
	it := library.LibraryItem{SourcePath: arg, SizeBytes: fi.Size()}
	switch ext {
	case ".iso":
		it.Platform = library.PlatformPS2
		base := strings.TrimSuffix(filepath.Base(arg), filepath.Ext(arg))
		it.Title = base
		hash, _, err := library.ContentHash(arg)
		if err != nil {
			return err
		}
		it.ContentHash = hash
		dt, m, err := library.Detect(&it, st, titledb)
		if err != nil {
			return err
		}
		it.DiscType, it.DetectionMethod = dt, m
	case ".cue":
		it.Platform = library.PlatformPS1
		it.Title = strings.TrimSuffix(filepath.Base(arg), filepath.Ext(arg))
	default:
		return fmt.Errorf("cannot inspect %q: want .iso or .cue", arg)
	}
	printItem(&it)
	return nil
}

func printItem(it *library.LibraryItem) {
	discType := string(it.DiscType)
	if discType == "" {
		discType = "-"
	}
	method := string(it.DetectionMethod)
	if method == "" {
		method = "-"
	}
	group := "-"
	if it.DiscGroupID != nil {
		group = fmt.Sprint(*it.DiscGroupID)
	}
	fmt.Printf("id:      %d\ntitle:   %s\nplatform: %s\ntype:    %s\nmethod:  %s\nsize:    %d\nstatus:  %s\ngroup:   %s\nsource:  %s\n",
		it.ID, it.Title, it.Platform, discType, method,
		it.SizeBytes, it.Status, group, it.SourcePath)
}
