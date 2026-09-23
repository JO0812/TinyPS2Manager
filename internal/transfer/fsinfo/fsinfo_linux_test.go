package fsinfo

import (
	"strings"
	"testing"
)

func TestParseMounts(t *testing.T) {
	mounts := `overlay / overlay rw,relatime 0 0
/dev/sda1 /mnt/stick vfat rw 0 0
/dev/sdb1 /mnt/stick2 exfat rw 0 0
tmpfs /tmp tmpfs rw 0 0
`
	for _, tc := range []struct {
		path      string
		mount     string
		fstype    string
		wantError bool
	}{
		{"/mnt/stick/GAME.ISO", "/mnt/stick", "vfat", false},
		{"/mnt/stick2/deep/dir", "/mnt/stick2", "exfat", false},
		{"/mnt/stick2x/file", "/", "overlay", false}, // boundary: not stick2
		{"/tmp/x", "/tmp", "tmpfs", false},
		{"/", "/", "overlay", false},
	} {
		mp, ft, err := parseMounts(strings.NewReader(mounts), tc.path)
		if tc.wantError {
			if err == nil {
				t.Errorf("%s: expected error", tc.path)
			}
			continue
		}
		if err != nil || mp != tc.mount || ft != tc.fstype {
			t.Errorf("%s = (%q,%q,%v), want (%q,%q)",
				tc.path, mp, ft, err, tc.mount, tc.fstype)
		}
	}
	if _, _, err := parseMounts(strings.NewReader(""), "/x"); err == nil {
		t.Error("empty mounts: expected error")
	}
}

func TestProbeLive(t *testing.T) {
	ft, free, total, err := Probe(t.TempDir())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if free <= 0 {
		t.Errorf("free = %d", free)
	}
	if total <= 0 || total < free {
		t.Errorf("total = %d, free = %d", total, free)
	}
	t.Logf("fstype=%q free=%d total=%d", ft, free, total)
}
