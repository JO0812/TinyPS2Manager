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

	titledb, err := library.LoadBundled()
	if err != nil {
		return err
	}
	if userPath, err := library.DefaultUserDBPath(); err == nil {
		if err := titledb.MergeUser(userPath); err != nil {
			return err
		}
	}
	stored, err := library.ImportDir(st, fs.Arg(0), titledb)
	if err != nil {
		return err
	}

	fmt.Printf("%-5s %-34s %-4s %-5s %-10s %12s %-7s %s\n",
		"ID", "TITLE", "PLAT", "TYPE", "METHOD", "SIZE", "STATUS", "GROUP")
	for _, it := range stored {
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
