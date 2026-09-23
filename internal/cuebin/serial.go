package cuebin

import (
	"encoding/binary"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// isoMaxCNFRead caps the SYSTEM.CNF read: the file is tiny by definition.
const (
	isoMaxCNFRead = 64 << 10
	serialPattern = `^[A-Z]{4}_[0-9]{3}\.[0-9]{2}$`
	bootSerialFind = `\\([A-Za-z]{4}_[0-9]{3}\.[0-9]{2})`
)

var (
	validSerial = regexp.MustCompile(serialPattern)
	bootSerial  = regexp.MustCompile(bootSerialFind)
)

// ExtractSerial reads the PS1 disc serial (e.g. SCUS_945.67) from a track
// image via its SYSTEM.CNF BOOT entry (spec §2.4: "extractable from the
// CUE/system area"). sectorSize is the image stride: 2048 for .iso files,
// 2352 for raw BIN track data (ISO structures live at stride multiples in
// both layouts). Only the PVD, the root directory, and the first 64 KiB of
// SYSTEM.CNF are read — the image is never loaded.
func ExtractSerial(r io.ReaderAt, size int64, sectorSize int) (string, error) {
	if sectorSize != 2048 && sectorSize != 2352 {
		return "", fmt.Errorf("bad sector size %d", sectorSize)
	}
	ss := int64(sectorSize)
	pvdOff := 16 * ss
	if size < pvdOff+ss {
		return "", fmt.Errorf("image too small for ISO9660 PVD (%d bytes)", size)
	}
	pvd := make([]byte, ss)
	if _, err := r.ReadAt(pvd, pvdOff); err != nil {
		return "", fmt.Errorf("read PVD: %w", err)
	}
	if pvd[0] != 1 || string(pvd[1:6]) != "CD001" {
		return "", fmt.Errorf("no ISO9660 primary volume descriptor at sector 16")
	}
	root, err := parseDirRecord(pvd[156:190])
	if err != nil {
		return "", fmt.Errorf("bad root directory record: %w", err)
	}
	cnf, err := findFile(r, size, root, ss, "SYSTEM.CNF;1")
	if err != nil {
		return "", err
	}
	content := make([]byte, min64(cnf.size, isoMaxCNFRead))
	if _, err := r.ReadAt(content, int64(cnf.extent)*ss); err != nil &&
		err != io.EOF {
		return "", fmt.Errorf("read SYSTEM.CNF: %w", err)
	}
	return serialFromCNF(content)
}

// dirExtent is an ISO9660 file location.
type dirExtent struct {
	extent uint32 // LBA sector
	size   int64
}

// parseDirRecord parses the fixed 34-byte directory record header. Longer
// records (long filenames) extend past hdr; the caller bounds them with the
// length byte.
func parseDirRecord(hdr []byte) (dirExtent, error) {
	if len(hdr) < 34 {
		return dirExtent{}, fmt.Errorf("record too short (%d bytes)", len(hdr))
	}
	if length := int(hdr[0]); length != 0 && length < 34 {
		return dirExtent{}, fmt.Errorf("bad record length %d", hdr[0])
	}
	return dirExtent{
		extent: binary.LittleEndian.Uint32(hdr[2:6]),
		size:   int64(binary.LittleEndian.Uint32(hdr[10:14])),
	}, nil
}

// findFile scans a directory extent for name and returns its location.
// stride is the image sector size (2048 or 2352).
func findFile(r io.ReaderAt, imgSize int64, dir dirExtent, stride int64, name string) (dirExtent, error) {
	end := int64(dir.extent)*stride + dir.size
	for off := int64(dir.extent) * stride; off < end; {
		hdr := make([]byte, 34)
		if _, err := r.ReadAt(hdr, off); err != nil {
			return dirExtent{}, fmt.Errorf("read dir record: %w", err)
		}
		recLen := int(hdr[0])
		if recLen == 0 {
			// Padding to the next sector.
			off = (off/stride + 1) * stride
			continue
		}
		if recLen < 34 {
			return dirExtent{}, fmt.Errorf("bad record length %d", recLen)
		}
		loc, err := parseDirRecord(hdr)
		if err != nil {
			return dirExtent{}, err
		}
		nameLen := int(hdr[32])
		if 33+nameLen > recLen {
			return dirExtent{}, fmt.Errorf("record name overruns length")
		}
		nameBuf := make([]byte, nameLen)
		if _, err := r.ReadAt(nameBuf, off+33); err != nil {
			return dirExtent{}, fmt.Errorf("read record name: %w", err)
		}
		if string(nameBuf) == name {
			if int64(loc.extent)*stride+loc.size > imgSize {
				return dirExtent{}, fmt.Errorf("%s extends past image end", name)
			}
			return loc, nil
		}
		off += int64(recLen)
	}
	return dirExtent{}, fmt.Errorf("%s not found", name)
}

// serialFromCNF extracts and validates the serial from SYSTEM.CNF content.
func serialFromCNF(content []byte) (string, error) {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 || !strings.EqualFold(strings.TrimSpace(kv[0]), "BOOT") {
			continue
		}
		m := bootSerial.FindStringSubmatch(strings.TrimSpace(kv[1]))
		if m == nil {
			return "", fmt.Errorf("BOOT entry has no disc serial")
		}
		serial := strings.ToUpper(m[1])
		if !validSerial.MatchString(serial) {
			return "", fmt.Errorf("malformed serial %q", m[1])
		}
		return serial, nil
	}
	return "", fmt.Errorf("no BOOT entry in SYSTEM.CNF")
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// ValidSerial reports whether s matches the PS1/PS2 disc-ID shape
// SXXX_NNN.NN (spec §2.4 filename convention).
func ValidSerial(s string) bool {
	return validSerial.MatchString(s)
}
