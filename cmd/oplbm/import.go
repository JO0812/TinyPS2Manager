package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jo/TinyPS2Manager/internal/library"
)

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	dbPath := fs.String("db", "", "SQLite library path (default $CONFIG/oplbm/oplbm.db)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm import [--db path] <dir>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("want exactly one source dir")
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

	items, err := library.ScanDir(fs.Arg(0))
	if err != nil {
		return err
	}
	titledb, err := library.LoadBundled()
	if err != nil {
		return err
	}
	if userPath, err := library.DefaultUserDBPath(); err == nil {
		if err := titledb.MergeUser(userPath); err != nil {
			return err
		}
	}

	// Persist, then remap temporary scan group IDs to real ones.
	tempGroups := map[int64]bool{}
	stored := make([]library.LibraryItem, 0, len(items))
	for _, it := range items {
		saved, err := st.UpsertItem(it)
		if err != nil {
			return err
		}
		if saved.DiscGroupID != nil && *saved.DiscGroupID < 0 {
			tempGroups[*saved.DiscGroupID] = true
		}
		stored = append(stored, saved)
	}
	remap := map[int64]int64{}
	for tmp := range tempGroups {
		real, err := st.NextGroupID()
		if err != nil {
			return err
		}
		if err := st.RemapGroup(tmp, real); err != nil {
			return err
		}
		remap[tmp] = real
	}

	// Detect PS2 types and record them (never clobbering overrides).
	fmt.Printf("%-5s %-34s %-4s %-5s %-10s %12s %-7s %s\n",
		"ID", "TITLE", "PLAT", "TYPE", "METHOD", "SIZE", "STATUS", "GROUP")
	for _, it := range stored {
		if it.Platform == library.PlatformPS2 {
			dt, m, err := library.Detect(&it, st, titledb)
			if err != nil {
				return err
			}
			if err := st.UpdateDetection(it.ID, dt, m); err != nil {
				return err
			}
			it.DiscType, it.DetectionMethod = dt, m
		}
		if it.DiscGroupID != nil {
			if real, ok := remap[*it.DiscGroupID]; ok {
				g := real
				it.DiscGroupID = &g
			} else {
				// Persisted with a pre-existing real group ID.
				cur, err := st.Get(it.ID)
				if err != nil {
					return err
				}
				it.DiscGroupID = cur.DiscGroupID
			}
		}
		group := "-"
		if it.DiscGroupID != nil {
			group = fmt.Sprint(*it.DiscGroupID)
		}
		discType := string(it.DiscType)
		if discType == "" {
			discType = "-"
		}
		method := string(it.DetectionMethod)
		if method == "" {
			method = "-"
		}
		fmt.Printf("%-5d %-34.34s %-4s %-5s %-10s %12d %-7s %s\n",
			it.ID, it.Title, it.Platform, discType, method,
			it.SizeBytes, it.Status, group)
	}
	return nil
}
