// Command gensrc writes synthetic M1 demo fixtures: deterministic filler
// bytes arranged as valid ISO9660/CUE structures, no game data, no binaries
// committed. Usage: go run ./testdata/gensrc --out <dir>.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	out := flag.String("out", "", "output directory (required)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "gensrc: --out required")
		os.Exit(2)
	}
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "gensrc:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	if err := os.MkdirAll(filepath.Join(out, "ps1"), 0o755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(out, "ps2cd.iso"),
		buildISO("TESTCD", 300000, "TEST_001.01", false)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(out, "ps2dvd.iso"),
		buildISO("TESTDVD", 5000000, "TEST_002.02", true)); err != nil {
		return err
	}
	// Single-disc PS1: 2-track BIN with patterned sectors.
	gameBin := makePatternedSectors(6)
	if err := writeFile(filepath.Join(out, "ps1", "game.bin"), gameBin); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(out, "ps1", "game.cue"),
		[]byte("FILE \"game.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"+
			"  TRACK 02 AUDIO\n    INDEX 00 00:00:04\n    INDEX 01 00:00:05\n")); err != nil {
		return err
	}
	// Multi-disc PS1 pair for the grouping demo.
	for i := 1; i <= 2; i++ {
		bin := fmt.Sprintf("Demo Quest (Disc %d).bin", i)
		cue := fmt.Sprintf("Demo Quest (Disc %d).cue", i)
		if err := writeFile(filepath.Join(out, "ps1", bin), makePatternedSectors(4)); err != nil {
			return err
		}
		sheet := fmt.Sprintf("FILE %q BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n", bin)
		if err := writeFile(filepath.Join(out, "ps1", cue), []byte(sheet)); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// makePatternedSectors returns n 2352-byte sectors; sector i is filled with
// byte(i+1) so merges stay verifiable.
func makePatternedSectors(n int) []byte {
	out := make([]byte, 0, n*2352)
	for i := 0; i < n; i++ {
		for j := 0; j < 2352; j++ {
			out = append(out, byte(i+1))
		}
	}
	return out
}

// buildISO assembles a tiny image with a PVD (label, volume sector count),
// an optional NSR02 bridge marker at sector 17, and a SYSTEM.CNF carrying
// serial. PVD size claims are descriptor-level only; the file stays small.
func buildISO(label string, sectors uint32, serial string, bridge bool) []byte {
	const ss = 2048
	img := make([]byte, 16*ss)
	pvd := make([]byte, ss)
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	for i := 40; i < 72; i++ {
		pvd[i] = ' '
	}
	copy(pvd[40:72], label)
	binary.LittleEndian.PutUint32(pvd[80:84], sectors)
	binary.BigEndian.PutUint32(pvd[84:88], sectors)
	rootSector := 17
	if bridge {
		rootSector = 18 // sector 17 holds the NSR bridge marker
	}
	copy(pvd[156:], dirRecord(uint32(rootSector), ss, 2, []byte{0}))
	img = append(img, pvd...)
	if bridge {
		nsr := make([]byte, ss)
		copy(nsr, "NSR02")
		img = append(img, nsr...)
		root := dirRecord(uint32(rootSector), ss, 2, []byte{0})
		root = appendRecord(root, uint32(rootSector), ss, 2, []byte{1})
		cnf := "BOOT = cdrom:\\" + serial + ";1\n"
		root = appendRecord(root, uint32(rootSector+1), uint32(len(cnf)), 0, []byte("SYSTEM.CNF;1"))
		sec := make([]byte, ss)
		copy(sec, root)
		img = append(img, sec...)
		dat := make([]byte, ss)
		copy(dat, cnf)
		img = append(img, dat...)
	} else {
		root := dirRecord(17, ss, 2, []byte{0})
		root = appendRecord(root, 17, ss, 2, []byte{1})
		cnf := "BOOT = cdrom:\\" + serial + ";1\n"
		root = appendRecord(root, 18, uint32(len(cnf)), 0, []byte("SYSTEM.CNF;1"))
		sec := make([]byte, ss)
		copy(sec, root)
		img = append(img, sec...)
		dat := make([]byte, ss)
		copy(dat, cnf)
		img = append(img, dat...)
	}
	return img
}

// dirRecord encodes one 33-fixed-byte ISO9660 directory record.
func dirRecord(extent, size uint32, flags byte, name []byte) []byte {
	return appendRecord(nil, extent, size, flags, name)
}

func appendRecord(buf []byte, extent, size uint32, flags byte, name []byte) []byte {
	recLen := 33 + len(name)
	if len(name)%2 == 0 {
		recLen++
	}
	rec := make([]byte, recLen)
	rec[0] = byte(recLen)
	binary.LittleEndian.PutUint32(rec[2:6], extent)
	binary.BigEndian.PutUint32(rec[6:10], extent)
	binary.LittleEndian.PutUint32(rec[10:14], size)
	binary.BigEndian.PutUint32(rec[14:18], size)
	rec[25] = flags
	rec[32] = byte(len(name))
	copy(rec[33:], name)
	return append(buf, rec...)
}
