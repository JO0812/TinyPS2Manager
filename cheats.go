package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/cheats"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

func cmdCheats(args []string) error {
	fs := flag.NewFlagSet("cheats", flag.ContinueOnError)
	dbPath := fs.String("db", "", "SQLite path (default $CONFIG/oplbm/oplbm.db)")
	var deviceID int64
	var confirm bool
	fs.Int64Var(&deviceID, "device", 0, "destination id")
	fs.BoolVar(&confirm, "confirm-uncertain", false, "confirm uncertain GameIDs")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm cheats --device <id> [--confirm-uncertain] [--db path]\n")
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
	// Try to load widescreen pack from a conventional location if present
	// (e.g., ./widescreen or $CONFIG/oplbm/widescreen). For now, just check
	// a local dir.
	wideDir := "widescreen"
	wideMap, _ := cheats.ParseWidescreenDir(wideDir)

	disk := transfer.FileDisk{}
	ctx := context.Background()
	staged := 0
	for _, it := range items {
		if it.Platform != library.PlatformPS2 || it.GameID == "" {
			continue
		}
		// Prefer widescreen if available, else try to find in database
		// For CLI, we just look for widescreen file; if not, skip (needs DB)
		var content string
		if data, ok := wideMap[it.GameID]; ok {
			content = string(data)
			fmt.Printf("using widescreen pack for %s (%s)\n", it.Title, it.GameID)
		} else {
			// Try to load from CheatDatabase.txt if present in cwd
			if _, err := os.Stat("CheatDatabase.txt"); err == nil {
				raw, _ := os.ReadFile("CheatDatabase.txt")
				dbMap, _ := cheats.ParseDatabase(raw)
				if g, ok := dbMap[it.Title]; ok {
					built, warns, err := cheats.Build(it.GameID, g.Cheats)
					if err != nil {
						fmt.Printf("skip %s: %v\n", it.Title, err)
						continue
					}
					if warns.DroppedCount > 0 {
						fmt.Printf("  dropped %d cheats (limit 250)\n", warns.DroppedCount)
					}
					if warns.HasEngineSkipped {
						fmt.Printf("  warning: relies on engine-skipped types 8/A/B\n")
					}
					content = built
				} else {
					continue
				}
			} else {
				continue
			}
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		// Validate via Build path already done for DB, for widescreen we still validate
		if _, _, err := cheats.Build(it.GameID, parseForValidate(content)); err != nil {
			// For widescreen, the file should already be valid; if not, error
			fmt.Printf("skip %s: validate failed: %v\n", it.Title, err)
			continue
		}
		// Stage
		// Need to check if file already exists via hand file
		chtPath := filepath.Join(dest.Path, dest.BDMPrefix, "CHT", it.GameID+".cht")
		if _, err := os.Stat(chtPath); err == nil {
			fmt.Printf("skip %s: hand file exists at %s\n", it.Title, chtPath)
			continue
		}
		if err := cheats.Stage(ctx, disk, dest.Path, dest.BDMPrefix, it, content, confirm); err != nil {
			if strings.Contains(err.Error(), "explicit confirm") {
				fmt.Printf("skip %s: %v (use --confirm-uncertain)\n", it.Title, err)
			} else {
				fmt.Printf("skip %s: stage failed: %v\n", it.Title, err)
			}
			continue
		}
		staged++
		fmt.Printf("staged %s -> %s\n", it.Title, it.GameID+".cht")
	}
	fmt.Printf("staged %d cheat files\n", staged)
	return nil
}

func parseForValidate(content string) []cheats.RawCheat {
	// Use cheats internal helper via re-parse: cheat file content is PS2RD format
	// We'll use a simple split: reuse Validate logic by calling internal parse
	// For now, just return a dummy that will pass if content has a master
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var cur *cheats.RawCheat
	var out []cheats.RawCheat
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if trim == "" || strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#") {
			continue
		}
		if len(trim) >= 17 && (trim[8] == ' ' || len(trim) == 17) {
			if cur != nil {
				cur.Codes = append(cur.Codes, trim)
			}
			continue
		}
		if cur != nil {
			out = append(out, *cur)
		}
		cur = &cheats.RawCheat{Name: trim}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}
