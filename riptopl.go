package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/riptopl"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

func cmdRiptopl(args []string) error {
	fs := flag.NewFlagSet("riptopl", flag.ContinueOnError)
	dbPath := fs.String("db", "", "SQLite path (default $CONFIG/oplbm/oplbm.db)")
	var deviceID int64
	var tag string
	fs.Int64Var(&deviceID, "device", 0, "destination id")
	fs.StringVar(&tag, "tag", riptopl.DefaultTag, "release tag (current-fan-favorite or rolling)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm riptopl --device <id> [--tag rolling] [--db path]\n")
		fs.PrintDefaults()
	}
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
	client := &riptopl.Client{}
	ctx := context.Background()
	rel, err := client.Resolve(ctx, tag)
	if err != nil {
		return err
	}
	fmt.Printf("resolved %s %s (%s) %s\n", rel.Tag, rel.AssetName, rel.Digest, rel.AssetURL)
	fmt.Printf("downloading %s (%d bytes)...\n", rel.AssetURL, rel.AssetSize)
	zipPath, err := client.Download(ctx, rel, func(done, total int64) {
		fmt.Printf("\r%d / %d", done, total)
	})
	if err != nil {
		return err
	}
	defer os.Remove(zipPath)
	fmt.Printf("\nstaging to %s (prefix %q)...\n", dest.Path, dest.BDMPrefix)
	st, err := riptopl.Stage(ctx, transfer.FileDisk{}, dest.Path, dest.BDMPrefix, zipPath, nil)
	if err != nil {
		return err
	}
	fmt.Printf("staged %s -> %s (%d bytes)\n", st.Flavour, st.ELFPath, st.ELFSize)
	fmt.Println("first-boot checklist:")
	for i, step := range riptopl.Checklist() {
		fmt.Printf("%d. %s\n", i+1, step)
	}
	// Record pinned version in queue state
	_ = qstore.SetState(fmt.Sprintf("loader.%d", dest.ID), fmt.Sprintf("%s %s %s", rel.Tag, rel.AssetName, rel.Digest))
	return nil
}
