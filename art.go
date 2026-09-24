package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/jo/TinyPS2Manager/internal/art"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

func cmdArt(args []string) error {
	fs := flag.NewFlagSet("art", flag.ContinueOnError)
	dbPath := fs.String("db", "", "SQLite path (default $CONFIG/oplbm/oplbm.db)")
	var deviceID int64
	var missingOnly bool
	fs.Int64Var(&deviceID, "device", 0, "destination id")
	fs.BoolVar(&missingOnly, "missing-only", false, "only fetch missing art")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm art --device <id> [--missing-only] [--db path]\n")
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
	lib, err := library.Open(db)
	if err != nil {
		return err
	}
	defer lib.Close()
	dest, err := qstore.GetDestination(deviceID)
	if err != nil {
		return err
	}
	if dest == nil {
		return fmt.Errorf("no destination %d", deviceID)
	}
	items, err := lib.List()
	if err != nil {
		return err
	}
	client := &art.Client{}
	ctx := context.Background()
	disk := transfer.FileDisk{}
	staged := 0
	for _, it := range items {
		key := art.KeyFor(it, "", "", false)
		// Check if art already exists (hand file wins)
		artPath := ""
		if dest.BDMPrefix != "" {
			artPath = dest.Path + "/" + dest.BDMPrefix + "/ART/" + key
		} else {
			artPath = dest.Path + "/ART/" + key
		}
		if missingOnly {
			if _, err := os.Stat(artPath); err == nil {
				continue
			}
		}
		var urls []string
		if it.Platform == library.PlatformPS2 {
			if it.GameID == "" {
				fmt.Printf("skip %s: no GameID\n", it.Title)
				continue
			}
			urls = art.PS2CoverURLs(it.GameID)
		} else {
			urls = art.PS1CoverURLs(it.Title)
		}
		if len(urls) == 0 {
			continue
		}
		artURL := urls[0]
		fmt.Printf("fetching %s -> %s\n", artURL, key)
		data, err := client.Fetch(ctx, artURL)
		if err != nil {
			fmt.Printf("  failed: %v\n", err)
			// Generate custom as fallback
			data = art.GenerateCustom(it.Title, it.Platform == library.PlatformPS2)
			fmt.Printf("  generated custom art (%d bytes)\n", len(data))
		} else {
			// Normalize
			if norm, err := art.ValidateAndNormalize(data, it.Platform == library.PlatformPS2); err == nil {
				data = norm
			}
		}
		if err := art.Stage(ctx, disk, dest.Path, dest.BDMPrefix, key, data); err != nil {
			fmt.Printf("  stage failed: %v\n", err)
			continue
		}
		staged++
		fmt.Printf("  staged %s\n", key)
	}
	fmt.Printf("staged %d covers\n", staged)
	return nil
}
