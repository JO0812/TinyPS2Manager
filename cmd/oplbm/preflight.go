package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jo/TinyPS2Manager/internal/queue"
)

func cmdPreflight(args []string) error {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	dbPath := fs.String("db", "", "SQLite path (default $CONFIG/oplbm/oplbm.db)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm preflight --device <id> [--db path]\n")
		fs.PrintDefaults()
	}
	var deviceID int64
	fs.Int64Var(&deviceID, "device", 0, "destination id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if deviceID == 0 {
		fs.Usage()
		return fmt.Errorf("--device is required")
	}
	db, err := resolveDB(*dbPath)
	if err != nil {
		return err
	}
	qstore, err := queue.Open(db)
	if err != nil {
		return err
	}
	defer qstore.Close()
	dest, err := qstore.GetDestination(deviceID)
	if err != nil {
		return err
	}
	if dest == nil {
		return fmt.Errorf("no destination %d", deviceID)
	}
	res, err := queue.Preflight(dest)
	if err != nil {
		return err
	}
	for _, c := range res.Checks {
		status := c.Status
		marker := "✓"
		if status == queue.CheckFail {
			marker = "✗"
		} else if status == queue.CheckWarn {
			marker = "!"
		}
		fmt.Printf("%s %-18s %-5s %s\n", marker, c.Name, status, c.Message)
	}
	if res.Blocked {
		fmt.Fprintln(os.Stderr, "preflight blocked: fix fail checks before enqueue")
		return fmt.Errorf("preflight blocked")
	}
	fmt.Println("preflight passed (warnings are non-blocking)")
	return nil
}
