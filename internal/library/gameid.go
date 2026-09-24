package library

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// gameIDRegex matches a BOOT2 serial like SLUS_213.85, SCUS_973.28 etc.
// The serial appears after cdrom0:\ and before ;1.
var boot2Re = regexp.MustCompile(`(?i)BOOT2\s*=\s*cdrom0:\\([A-Z]{4}_\d{3}\.\d{2})`)

const (
	sectorSize  = 2048
	pvdSector   = 16
	pvdRootOff  = 156
	pvdRootLen  = 34
)

// ExtractGameID streams SYSTEM.CNF out of path (ISO) and parses BOOT2.
// It returns gameID (e.g. SLUS_213.85) and uncertain flag. Uncertain is
// true when the serial was extracted but the ISO appears to be a mod/
// translation that reuses a base-game serial (heuristic: SYSTEM.CNF
// contains markers like MOD, TRANSLATION, PATCH, HACK alongside a valid
// serial). Callers treat uncertain as "needs explicit user confirm before
// enrichment" (spec §2.9 #3). Streaming: only the PVD, the root directory
// extent and SYSTEM.CNF are read.
func ExtractGameID(path string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	if fi.Size() < (pvdSector+1)*sectorSize {
		return "", false, fmt.Errorf("file too small to be ISO")
	}
	r := bufio.NewReaderSize(f, 64*1024)
	pvd, err := readSector(r, f, pvdSector)
	if err != nil {
		return "", false, err
	}
	if !isPVD(pvd) {
		return "", false, fmt.Errorf("no ISO9660 PVD at sector %d", pvdSector)
	}
	// Root directory record at 156 in PVD.
	if len(pvd) < pvdRootOff+pvdRootLen {
		return "", false, fmt.Errorf("PVD truncated")
	}
	rec := pvd[pvdRootOff : pvdRootOff+pvdRootLen]
	extent := binary.LittleEndian.Uint32(rec[2:6])
	dataLen := binary.LittleEndian.Uint32(rec[10:14])
	if dataLen == 0 || dataLen > 1<<20 { // root dir should be tiny; sanity cap
		return "", false, fmt.Errorf("root directory size %d out of range", dataLen)
	}
	dirData, err := readExtent(r, f, int64(extent), int64(dataLen))
	if err != nil {
		return "", false, err
	}
	// Find SYSTEM.CNF;1
	var sysExtent uint32
	var sysSize uint32
	found := false
	for off := 0; off < len(dirData); {
		if off >= len(dirData) {
			break
		}
		rl := int(dirData[off])
		if rl == 0 {
			// Pad to next sector.
			next := ((off / sectorSize) + 1) * sectorSize
			if next <= off {
				break
			}
			off = next
			continue
		}
		if off+rl > len(dirData) || rl < 33 {
			break
		}
		entry := dirData[off : off+rl]
		// File flags at 25, identifier length at 32
		idLen := int(entry[32])
		if 33+idLen > len(entry) {
			off += rl
			continue
		}
		rawID := string(entry[33 : 33+idLen])
		// Strip version after ';' and handle ;1
		baseID := strings.Split(rawID, ";")[0]
		// ISO9660 pads odd-length identifiers with 0; also directory entries
		// use 0x00 and 0x01 for . and ..
		if strings.EqualFold(baseID, "SYSTEM.CNF") {
			sysExtent = binary.LittleEndian.Uint32(entry[2:6])
			sysSize = binary.LittleEndian.Uint32(entry[10:14])
			found = true
			break
		}
		off += rl
	}
	if !found {
		return "", false, fmt.Errorf("SYSTEM.CNF not found in ISO")
	}
	if sysSize == 0 || sysSize > 64*1024 {
		return "", false, fmt.Errorf("SYSTEM.CNF size %d out of range", sysSize)
	}
	cnfData, err := readExtent(r, f, int64(sysExtent), int64(sysSize))
	if err != nil {
		return "", false, err
	}
	// SYSTEM.CNF may have null padding; trim.
	cnfStr := string(bytes.Trim(cnfData, "\x00\r\n "))
	m := boot2Re.FindStringSubmatch(cnfStr)
	if m == nil {
		return "", false, fmt.Errorf("BOOT2 line not found in SYSTEM.CNF")
	}
	serial := strings.ToUpper(m[1])
	// Validate via cuebin helper would need import cycle; do simple check.
	if !validSerial(serial) {
		return "", false, fmt.Errorf("BOOT2 serial %q invalid", serial)
	}
	uncertain := isUncertainMod(cnfStr, serial)
	return serial, uncertain, nil
}

func validSerial(s string) bool {
	if len(s) != 11 { // e.g. SLUS_213.85
		return false
	}
	if s[4] != '_' || s[8] != '.' {
		return false
	}
	for i, c := range s {
		switch {
		case i < 4:
			if c < 'A' || c > 'Z' {
				return false
			}
		case i == 4, i == 8:
			// '_' and '.'
		default:
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func isUncertainMod(cnf, serial string) bool {
	upper := strings.ToUpper(cnf)
	markers := []string{"MOD", "TRANSLATION", "PATCH", "HACK", "UNDUB", "REMASTER"}
	for _, mk := range markers {
		if strings.Contains(upper, mk) {
			// If the CNF contains a mod marker alongside a valid base-game
			// serial, the serial likely names the base game, not the mod.
			return true
		}
	}
	// Also consider BOOT2 line that contains extra suffix after serial before ;1
	// e.g. SLUS_213.85_MOD;1 — the regex still captures base serial, but suffix indicates mod.
	if strings.Contains(strings.ToUpper(cnf), strings.ToUpper(serial)+"_") || strings.Contains(strings.ToUpper(cnf), strings.ToUpper(serial)+"-") {
		return true
	}
	return false
}

func isPVD(buf []byte) bool {
	return len(buf) >= sectorSize && buf[0] == 1 && string(buf[1:6]) == "CD001" && buf[6] == 1
}

func readSector(r *bufio.Reader, f *os.File, sector int64) ([]byte, error) {
	if _, err := f.Seek(sector*sectorSize, io.SeekStart); err != nil {
		return nil, err
	}
	r.Reset(f)
	buf := make([]byte, sectorSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func readExtent(r *bufio.Reader, f *os.File, sector int64, size int64) ([]byte, error) {
	if _, err := f.Seek(sector*sectorSize, io.SeekStart); err != nil {
		return nil, err
	}
	r.Reset(f)
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
