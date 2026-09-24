// Package main is the oplbm command-line interface (M1 milestone owner:
// import, inspect, convert, split, tree against the real engines).
//
// The HTTP API landed in M2; the Wails desktop shell (build tag `desktop`,
// see desktop.go) in M4. This CLI stays as the scriptable, testable front
// end afterwards.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		usage()
		return 2
	}
	var err error
	switch args[0] {
	case "import":
		err = cmdImport(args[1:])
	case "inspect":
		err = cmdInspect(args[1:])
	case "convert":
		err = cmdConvert(args[1:])
	case "split":
		err = cmdSplit(args[1:])
	case "tree":
		err = cmdTree(args[1:])
	case "serve":
		err = cmdServe(args[1:])
	case "gameid":
		err = cmdGameID(args[1:])
	case "art":
		err = cmdArt(args[1:])
	case "cheats":
		err = cmdCheats(args[1:])
	case "riptopl":
		err = cmdRiptopl(args[1:])
	case "preflight":
		err = cmdPreflight(args[1:])
	case "-h", "-help", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		usage()
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprintf(os.Stderr, `oplbm — OPL/POPSTARTER backup manager (M1 CLI)

Usage:
  oplbm import [--db path] <dir>                scan sources, detect, persist
  oplbm inspect [--db path] <id|path>           show detected type + method
  oplbm convert --out <dir> [--db path] <ps1-id|.cue>
                                            CUE/BIN -> VCD
  oplbm split --out <dir> [--name N] [--serial S] <iso>
                                            ISO -> USBExtreme set at out root
  oplbm tree --plan <jobs.json> --dest <root> [--prefix P]
                                            dry-run the destination tree
  oplbm serve [--bind addr] [--db path] [--settings path]
                                            REST + SSE API on localhost
  oplbm gameid <iso>                            print PS2 serial (SYSTEM.CNF BOOT2)
  oplbm art --device <id> [--missing-only] [--db path]
                                            fetch & stage cover art (ART/)
  oplbm cheats --device <id> [--confirm-uncertain] [--db path]
                                            build & stage .cht cheats (CHT/)
  oplbm riptopl --device <id> [--tag rolling] [--db path]
                                            download & stage RIPTOPL.ELF (APPS/)
  oplbm preflight --device <id> [--db path]     run drive pre-flight checks
(note: flags must precede positional args — Go flag convention)

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
jobs.json schema:
  {"files": [{"bucket": "DVD|CD|POPS|...", "subdir": "", "name": "Game.iso"}],
   "multidisc": [{"vcds": ["A.VCD", "B.VCD"], "vmcdir": "A"}]}
`)
}

// defaultDBPath is the SQLite library location, overridable per command.
func defaultDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "oplbm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "oplbm.db"), nil
}

// resolveDB returns the --db flag value or the default path.
func resolveDB(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	return defaultDBPath()
}
