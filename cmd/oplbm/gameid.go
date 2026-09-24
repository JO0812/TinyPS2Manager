package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jo/TinyPS2Manager/internal/library"
)

func cmdGameID(args []string) error {
	fs := flag.NewFlagSet("gameid", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm gameid <iso>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("want exactly one iso path")
	}
	path := fs.Arg(0)
	gid, uncertain, err := library.ExtractGameID(path)
	if err != nil {
		return err
	}
	if uncertain {
		fmt.Printf("%s (uncertain)\n", gid)
	} else {
		fmt.Println(gid)
	}
	return nil
}
