package library

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func rec(buf []byte, extent, size uint32, flags byte, name []byte) []byte {
	recLen := 33 + len(name)
	if len(name)%2 == 0 {
		recLen++
	}
	r := make([]byte, recLen)
	r[0] = byte(recLen)
	binary.LittleEndian.PutUint32(r[2:6], extent)
	binary.LittleEndian.PutUint32(r[10:14], size)
	r[25] = flags
	r[32] = byte(len(name))
	copy(r[33:], name)
	return append(buf, r...)
}

func makeTestISO(serial string, extra string) []byte {
	const ss = 2048
	img := make([]byte, 16*ss)
	pvd := make([]byte, ss)
	pvd[0] = 1
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	copy(pvd[40:72], "TESTVOL")
	binary.LittleEndian.PutUint32(pvd[80:84], 3)
	copy(pvd[156:], rec(nil, 17, ss, 2, []byte{0}))
	img = append(img, pvd...)
	cnf := fmt.Sprintf("BOOT2 = cdrom0:\\%s;1\n", serial)
	if extra != "" {
		cnf += extra + "\n"
	}
	root := rec(nil, 17, ss, 2, []byte{0})
	root = rec(root, 17, ss, 2, []byte{1})
	root = rec(root, 18, uint32(len(cnf)), 0, []byte("SYSTEM.CNF;1"))
	sec := make([]byte, ss)
	copy(sec, root)
	img = append(img, sec...)
	dat := make([]byte, ss)
	copy(dat, cnf)
	img = append(img, dat...)
	return img
}

func TestExtractGameID(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test.iso")
	serial := "SLUS_213.85"
	iso := makeTestISO(serial, "")
	if err := os.WriteFile(tmp, iso, 0o644); err != nil {
		t.Fatal(err)
	}
	gid, uncertain, err := ExtractGameID(tmp)
	if err != nil {
		t.Fatalf("ExtractGameID: %v", err)
	}
	if gid != serial {
		t.Errorf("gid = %q, want %q", gid, serial)
	}
	if uncertain {
		t.Error("uncertain should be false for clean ISO")
	}
}

func TestExtractGameIDUncertain(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "mod.iso")
	serial := "SLUS_213.85"
	iso := makeTestISO(serial, "# MOD TRANSLATION PATCH")
	if err := os.WriteFile(tmp, iso, 0o644); err != nil {
		t.Fatal(err)
	}
	gid, uncertain, err := ExtractGameID(tmp)
	if err != nil {
		t.Fatalf("ExtractGameID mod: %v", err)
	}
	if gid != serial {
		t.Errorf("gid = %q, want %q", gid, serial)
	}
	if !uncertain {
		t.Error("uncertain should be true for mod ISO")
	}
}

func TestExtractGameIDMissing(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "tiny.iso")
	if err := os.WriteFile(tmp, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExtractGameID(tmp); err == nil {
		t.Error("tiny file: expected error")
	}
}

func TestValidSerial(t *testing.T) {
	if !validSerial("SLUS_213.85") {
		t.Error("valid serial rejected")
	}
	if validSerial("BADSerial") {
		t.Error("invalid serial accepted")
	}
}
