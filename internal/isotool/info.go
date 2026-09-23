package isotool

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// ISO9660/UDF geometry.
const (
	sectorSize   = 2048
	pvdSector    = 16
	udfScanLimit = 64 // sectors scanned for the UDF recognition sequence
)

// Confidence grades how much the descriptors told us.
type Confidence string

const (
	// ConfidenceHigh: a valid ISO9660 PVD was parsed (label + size known).
	ConfidenceHigh Confidence = "high"
	// ConfidenceLow: UDF recognized but no ISO9660 PVD (size unknown).
	ConfidenceLow Confidence = "low"
	// ConfidenceNone: no descriptors recognized; Inspect returns an error.
	ConfidenceNone Confidence = "none"
)

// ISOInfo is the read-only inspection result for one image.
type ISOInfo struct {
	VolumeLabel         string
	SectorSize          int
	SectorCount         uint32
	DataSizeBytes       int64
	HasISO9660          bool
	HasUDF              bool
	DetectionConfidence Confidence
}

// Inspect opens path read-only and parses its volume descriptors without
// loading the image. It returns an error when neither ISO9660 nor UDF is
// recognized; callers treat that as "inspection inconclusive" and fall back
// to the size heuristic (spec §2.3 #3).
func Inspect(path string) (ISOInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return ISOInfo{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ISOInfo{}, err
	}
	r := bufio.NewReaderSize(f, 64<<10)

	info := ISOInfo{SectorSize: sectorSize, DetectionConfidence: ConfidenceNone}
	if pvd, err := readSector(r, f, pvdSector); err == nil && isPVD(pvd) {
		info.HasISO9660 = true
		info.DetectionConfidence = ConfidenceHigh
		info.VolumeLabel = strings.TrimRight(string(pvd[40:72]), " ")
		info.SectorCount = binary.LittleEndian.Uint32(pvd[80:84])
		info.DataSizeBytes = int64(info.SectorCount) * sectorSize
	}
	if has, err := scanUDF(r, f, fi.Size()); err == nil && has {
		info.HasUDF = true
		if !info.HasISO9660 {
			info.DetectionConfidence = ConfidenceLow
		}
	}
	if !info.HasISO9660 && !info.HasUDF {
		return ISOInfo{}, fmt.Errorf("%s: no ISO9660 or UDF descriptors recognized", path)
	}
	return info, nil
}

// readSector returns a full 2048-byte sector via buffered reads.
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

// isPVD reports whether buf is an ISO9660 primary volume descriptor.
func isPVD(buf []byte) bool {
	return len(buf) >= sectorSize && buf[0] == 1 &&
		string(buf[1:6]) == "CD001" && buf[6] == 1
}

// scanUDF reports whether the NSR02/NSR03 recognition sequence appears in
// the first udfScanLimit sectors. Only the 5-byte magic of each sector is
// compared; the rest is skipped without reading.
func scanUDF(r *bufio.Reader, f *os.File, fileSize int64) (bool, error) {
	magic := make([]byte, 5)
	for sector := int64(0); sector < udfScanLimit; sector++ {
		if sector*sectorSize+5 > fileSize {
			return false, nil
		}
		if _, err := f.Seek(sector*sectorSize, io.SeekStart); err != nil {
			return false, err
		}
		r.Reset(f)
		if _, err := io.ReadFull(r, magic); err != nil {
			return false, nil // truncated tail: no UDF here
		}
		if string(magic) == "NSR02" || string(magic) == "NSR03" {
			return true, nil
		}
	}
	return false, nil
}
