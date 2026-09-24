package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jo/TinyPS2Manager/internal/oplfs"
)

// planFile is the dry-run input schema (see usage text).
type planFile struct {
	Files []struct {
		Bucket string `json:"bucket"`
		Subdir string `json:"subdir"`
		Name   string `json:"name"`
	} `json:"files"`
	MultiDisc []struct {
		VCDs   []string `json:"vcds"`
		VMCDir string   `json:"vmcdir"`
	} `json:"multidisc"`
}

func cmdTree(args []string) error {
	fs := flag.NewFlagSet("tree", flag.ContinueOnError)
	planPath := fs.String("plan", "", "jobs JSON file (required)")
	dest := fs.String("dest", "", "destination root (required)")
	prefix := fs.String("prefix", "", "BDM prefix subfolder")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm tree --plan <jobs.json> --dest <root> [--prefix P]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *planPath == "" || *dest == "" {
		fs.Usage()
		return fmt.Errorf("want --plan and --dest")
	}
	raw, err := os.ReadFile(*planPath)
	if err != nil {
		return err
	}
	var pf planFile
	if err := json.Unmarshal(raw, &pf); err != nil {
		return fmt.Errorf("parse %s: %w", *planPath, err)
	}

	plan := &oplfs.TreePlan{Root: *dest, BDMprefix: *prefix}
	for _, f := range pf.Files {
		plan.Add(oplfs.Bucket(f.Bucket), f.Subdir, f.Name)
	}
	for _, md := range pf.MultiDisc {
		set := oplfs.MultiDiscSet{VCDs: md.VCDs, VMCDir: md.VMCDir}
		files, dirs, err := set.Artifacts()
		if err != nil {
			return fmt.Errorf("multidisc %v: %w", md.VCDs, err)
		}
		plan.Files = append(plan.Files, files...)
		for _, d := range dirs {
			plan.ExtraDirs = append(plan.ExtraDirs, d)
		}
	}
	dirs, files, err := plan.Paths()
	if err != nil {
		return fmt.Errorf("invalid plan: %w", err)
	}
	fmt.Println("DIRS:")
	for _, d := range dirs {
		fmt.Printf("  %s\n", d)
	}
	fmt.Println("FILES:")
	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}
	return nil
}
