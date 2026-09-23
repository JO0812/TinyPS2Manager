package cuebin

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

type isoFile struct {
	name    string
	content string
}

// buildTestISO assembles a minimal ISO9660 image at the given sector stride:
// 16 empty sectors, a PVD at sector 16, the root directory at 17, and one
// sector per file from 18 on.
func buildTestISO(t *testing.T, stride int, files []isoFile) []byte {
	t.Helper()
	img := make([]byte, 16*stride)
	pvd := make([]byte, stride)
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	rootRec := appendRecord(nil, 17, uint32(stride), 2, []byte{0})
	copy(pvd[156:], rootRec)
	img = append(img, pvd...)

	root := []byte{}
	root = appendRecord(root, 17, uint32(stride), 2, []byte{0})
	root = appendRecord(root, 17, uint32(stride), 2, []byte{1})
	extent := uint32(18)
	for _, f := range files {
		root = appendRecord(root, extent, uint32(len(f.content)), 0, []byte(f.name))
		extent++
	}
	rootPadded := make([]byte, stride)
	copy(rootPadded, root)
	img = append(img, rootPadded...)
	for _, f := range files {
		sec := make([]byte, stride)
		copy(sec, f.content)
		img = append(img, sec...)
	}
	return img
}

// appendRecord encodes one ISO9660 directory record: 33 fixed bytes
// (length, ext-attr, extent/le+be, size/le+be, date, flags, unit, gap,
// volume-seq/le+be, name length) followed by the name, plus a pad byte when
// the name length is even.
func appendRecord(buf []byte, extent, size uint32, flags byte, name []byte) []byte {
	recLen := 33 + len(name)
	if len(name)%2 == 0 {
		recLen++ // pad byte keeps the record even-aligned
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

func extractFromISO(t *testing.T, img []byte, stride int) (string, error) {
	t.Helper()
	return ExtractSerial(bytes.NewReader(img), int64(len(img)), stride)
}

func TestExtractSerial(t *testing.T) {
	img := buildTestISO(t, 2048, []isoFile{{
		"SYSTEM.CNF;1",
		"BOOT = cdrom:\\SCUS_945.67;1\nTCB = 4\nEVENT = 10\nSTACK = 801FFF00\n",
	}})
	got, err := extractFromISO(t, img, 2048)
	if err != nil {
		t.Fatalf("ExtractSerial: %v", err)
	}
	if got != "SCUS_945.67" {
		t.Errorf("serial = %q, want SCUS_945.67", got)
	}
}

func TestExtractSerialLowercase(t *testing.T) {
	img := buildTestISO(t, 2048, []isoFile{{
		"SYSTEM.CNF;1",
		"boot = cdrom:\\slus_123.45;1\n",
	}})
	got, err := extractFromISO(t, img, 2048)
	if err != nil {
		t.Fatalf("ExtractSerial: %v", err)
	}
	if got != "SLUS_123.45" {
		t.Errorf("serial = %q, want SLUS_123.45", got)
	}
}

func TestExtractSerialRawTrack(t *testing.T) {
	// Raw BIN track data is 2352-stride: the same structures at wider offsets.
	img := buildTestISO(t, 2352, []isoFile{{
		"SYSTEM.CNF;1",
		"BOOT = cdrom:\\SCUS_945.67;1\n",
	}})
	got, err := extractFromISO(t, img, 2352)
	if err != nil {
		t.Fatalf("ExtractSerial: %v", err)
	}
	if got != "SCUS_945.67" {
		t.Errorf("serial = %q, want SCUS_945.67", got)
	}
	// A 2352 image read at the wrong stride must not silently succeed.
	if _, err := extractFromISO(t, img, 2048); err == nil {
		t.Error("wrong stride: expected error, got nil")
	}
}

func TestExtractSerialErrors(t *testing.T) {
	full := "BOOT = cdrom:\\SCUS_945.67;1\n"
	cases := map[string]struct {
		img  []byte
		want string
	}{
		"missing cnf": {
			buildTestISO(t, 2048, []isoFile{{"OTHER.DAT;1", "x"}}),
			"not found",
		},
		"no boot entry": {
			buildTestISO(t, 2048, []isoFile{{"SYSTEM.CNF;1", "TCB = 4\n"}}),
			"no BOOT entry",
		},
		"boot without serial": {
			buildTestISO(t, 2048, []isoFile{{"SYSTEM.CNF;1", "BOOT = cdrom:\\;1\n"}}),
			"no disc serial",
		},
		"not iso":     {make([]byte, 64<<10), "no ISO9660"},
		"truncated":   {make([]byte, 100), "too small"},
		"empty image": {nil, "too small"},
		"bad stride":  {make([]byte, 64<<10), "bad sector size"},
	}
	_ = full
	for name, tc := range cases {
		stride := 2048
		if name == "bad stride" {
			stride = 1000
		}
		if _, err := extractFromISO(t, tc.img, stride); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		} else if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q lacks %q", name, err, tc.want)
		}
	}
}

func TestValidSerial(t *testing.T) {
	for _, good := range []string{"SCUS_945.67", "SLUS_213.85", "SCES_000.01"} {
		if !ValidSerial(good) {
			t.Errorf("%q should be valid", good)
		}
	}
	for _, bad := range []string{"", "SCUS945.67", "SCUS_9456.7", "scus_945.67",
		"SCUS_945.67;1", "SCUS_945.678", "ABC_123.45"} {
		if ValidSerial(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
