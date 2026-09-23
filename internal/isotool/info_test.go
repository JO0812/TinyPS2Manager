package isotool

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makePVD builds a minimal primary volume descriptor with the given label
// (space-padded to 32) and volume space size.
func makePVD(label string, sectors uint32) []byte {
	pvd := make([]byte, sectorSize)
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	for i := 40; i < 72; i++ {
		pvd[i] = ' '
	}
	copy(pvd[40:72], label)
	binary.LittleEndian.PutUint32(pvd[80:84], sectors)
	binary.BigEndian.PutUint32(pvd[84:88], sectors)
	return pvd
}

// writeImage writes sectors (each exactly 2048 bytes) to a temp file.
func writeImage(t *testing.T, sectors ...[]byte) string {
	t.Helper()
	var img []byte
	for _, s := range sectors {
		if len(s) != sectorSize {
			t.Fatalf("sector size %d, want %d", len(s), sectorSize)
		}
		img = append(img, s...)
	}
	path := filepath.Join(t.TempDir(), "test.iso")
	if err := os.WriteFile(path, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func blank() []byte { return make([]byte, sectorSize) }

func TestInspectISOOnly(t *testing.T) {
	path := writeImage(t, blank(), blank(), blank(), blank(), blank(), blank(),
		blank(), blank(), blank(), blank(), blank(), blank(), blank(), blank(),
		blank(), blank(), makePVD("TEST_LABEL", 1000))
	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !info.HasISO9660 || info.HasUDF {
		t.Errorf("flags = iso:%v udf:%v, want true,false", info.HasISO9660, info.HasUDF)
	}
	if info.DetectionConfidence != ConfidenceHigh {
		t.Errorf("confidence = %q, want high", info.DetectionConfidence)
	}
	if info.VolumeLabel != "TEST_LABEL" {
		t.Errorf("label = %q", info.VolumeLabel)
	}
	if info.SectorSize != 2048 || info.SectorCount != 1000 {
		t.Errorf("geometry = %d x %d", info.SectorSize, info.SectorCount)
	}
	if info.DataSizeBytes != 1000*2048 {
		t.Errorf("data size = %d", info.DataSizeBytes)
	}
}

func TestInspectBridge(t *testing.T) {
	nsr := blank()
	copy(nsr, "NSR02")
	path := writeImage(t,
		blank(), blank(), blank(), blank(), blank(), blank(), blank(), blank(),
		blank(), blank(), blank(), blank(), blank(), blank(), blank(), blank(),
		makePVD("DVD_VIDEO", 2000000), nsr)
	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !info.HasISO9660 || !info.HasUDF {
		t.Errorf("flags = iso:%v udf:%v, want true,true", info.HasISO9660, info.HasUDF)
	}
	if info.DataSizeBytes != 2000000*2048 {
		t.Errorf("data size = %d", info.DataSizeBytes)
	}
}

func TestInspectUDFOnly(t *testing.T) {
	nsr := blank()
	copy(nsr, "NSR03")
	path := writeImage(t, blank(), blank(), blank(), blank(), blank(), nsr)
	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.HasISO9660 || !info.HasUDF {
		t.Errorf("flags = iso:%v udf:%v, want false,true", info.HasISO9660, info.HasUDF)
	}
	if info.DetectionConfidence != ConfidenceLow {
		t.Errorf("confidence = %q, want low", info.DetectionConfidence)
	}
}

func TestInspectErrors(t *testing.T) {
	garbage := blank()
	for i := range garbage {
		garbage[i] = byte(i)
	}
	if _, err := Inspect(writeImage(t, garbage, garbage)); err == nil {
		t.Error("garbage: expected error, got nil")
	} else if !strings.Contains(err.Error(), "no ISO9660 or UDF") {
		t.Errorf("garbage: error %q", err)
	}
	tiny := filepath.Join(t.TempDir(), "tiny.iso")
	if err := os.WriteFile(tiny, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(tiny); err == nil {
		t.Error("truncated: expected error, got nil")
	}
	if _, err := Inspect(filepath.Join(t.TempDir(), "missing.iso")); err == nil {
		t.Error("missing file: expected error, got nil")
	}
}

func TestInspectFullLabel(t *testing.T) {
	label := strings.Repeat("A", 32) // no padding to trim
	path := writeImage(t,
		blank(), blank(), blank(), blank(), blank(), blank(), blank(), blank(),
		blank(), blank(), blank(), blank(), blank(), blank(), blank(), blank(),
		makePVD(label, 10))
	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.VolumeLabel != label {
		t.Errorf("label = %q", info.VolumeLabel)
	}
}
