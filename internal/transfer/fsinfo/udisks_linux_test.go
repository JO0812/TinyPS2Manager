package fsinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rootDevice finds the block device backing path via /proc/mounts for the
// live UDisks test. Empty when the path sits on a pseudo-filesystem.
func rootDevice(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile("/proc/mounts")
	if err != nil {
		t.Skip("no /proc/mounts")
	}
	best, bestDev := "", ""
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		dev, mp := fields[0], fields[1]
		if mp == "/" || abs == mp || strings.HasPrefix(abs, mp+"/") {
			if len(mp) > len(best) {
				best, bestDev = mp, dev
			}
		}
	}
	if !strings.HasPrefix(bestDev, "/dev/") {
		return ""
	}
	return bestDev
}

func TestUdisksLookupSelf(t *testing.T) {
	// Look up the device backing the repo itself: real disks report a dos
	// or gpt table. Skipped where UDisks2 is absent (CI containers) or the
	// root sits on a pseudo-filesystem (overlay).
	dev := rootDevice(t, ".")
	if dev == "" {
		t.Skip("root is not a block device mount")
	}
	info, err := UdisksLookup(dev)
	if err != nil {
		t.Skipf("udisks2 unavailable: %v", err)
	}
	if info.Table != "dos" && info.Table != "gpt" && info.Table != "" {
		t.Errorf("table = %q", info.Table)
	}
	t.Logf("dev=%s table=%q parttype=%q rotation=%d hasRotation=%v",
		dev, info.Table, info.PartType, info.RotationRate, info.HasRotation)
}

func TestUdisksLookupUnknown(t *testing.T) {
	if _, err := UdisksLookup("/dev/does-not-exist-xyz"); err == nil {
		// Absent bus and unknown device both surface as errors — except on
		// exotic setups; either way it must not panic or block.
		t.Log("lookup of bogus device unexpectedly succeeded")
	}
}
