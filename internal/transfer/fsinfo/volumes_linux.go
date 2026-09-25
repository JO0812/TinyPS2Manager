//go:build linux

package fsinfo

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Volumes lists mounted external drives on Linux by scanning /proc/mounts
// (pure Go, no shelling): entries whose device is a real block device
// (/dev/…) mounted under /media/, /run/media/ (udisks) or /mnt/ qualify.
// Mounts under /media or /run/media count as Removable (udisks-managed);
// /mnt entries are listed but not flagged removable.
func Volumes() ([]Volume, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return volumesFromMounts(f)
}

type mountEntry struct {
	device string
	mount  string
	fstype string
}

func parseVolumeEntries(r io.Reader) []mountEntry {
	var out []mountEntry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		out = append(out, mountEntry{device: fields[0], mount: fields[1], fstype: fields[2]})
	}
	return out
}

// removableDevice reports whether a /dev node looks like a directly
// attached disk: USB sticks/HDDs (sda, sda1), SD/MMC cards (mmcblk0p1),
// NVMe (nvme0n1p1), virtio/IDE disks (vda1, hda1). Whole disks qualify
// too (superfloppy sticks mount /dev/sda directly). Loops,
// device-mapper, and pseudo devices never qualify.
func removableDevice(dev string) bool {
	base := filepath.Base(dev)
	for _, prefix := range []string{"sd", "mmcblk", "nvme", "vd", "hd"} {
		rest, ok := strings.CutPrefix(base, prefix)
		if !ok || rest == "" {
			continue
		}
		alnum := true
		for i := 0; i < len(rest); i++ {
			c := rest[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
				alnum = false
				break
			}
		}
		if alnum {
			return true
		}
	}
	return false
}

func underAny(path string, prefixes ...string) bool {
	for _, p := range prefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func volumesFromMounts(r io.Reader) ([]Volume, error) {
	seen := map[string]bool{}
	var out []Volume
	for _, e := range parseVolumeEntries(r) {
		if !strings.HasPrefix(e.device, "/dev/") || !removableDevice(e.device) {
			continue
		}
		if !underAny(e.mount, "/media", "/run/media", "/mnt") {
			continue
		}
		if seen[e.mount] {
			continue // same mount listed twice (bind mounts)
		}
		seen[e.mount] = true
		v := Volume{
			Path:       e.mount,
			Label:      resolveLabel(e.device, filepath.Base(e.mount)),
			Filesystem: e.fstype,
			FreeBytes:  -1,
			TotalBytes: -1,
			Removable:  underAny(e.mount, "/media", "/run/media"),
		}
		var st unix.Statfs_t
		if err := unix.Statfs(e.mount, &st); err == nil {
			v.FreeBytes = int64(st.Bavail) * int64(st.Bsize)
			v.TotalBytes = int64(st.Blocks) * int64(st.Bsize)
		}
		out = append(out, v)
	}
	return out, nil
}

// resolveLabel returns the filesystem label for device by matching
// /dev/disk/by-label symlinks; falls back to the mount basename (what
// udisks shows, e.g. 34FF-D9F8) when unresolvable. Best-effort: never
// errors.
func resolveLabel(device, fallback string) string {
	entries, err := os.ReadDir("/dev/disk/by-label")
	if err != nil {
		return fallback
	}
	want, err := filepath.EvalSymlinks(device)
	if err != nil {
		want = device
	}
	for _, e := range entries {
		got, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-label", e.Name()))
		if err != nil {
			continue
		}
		if got == want {
			return e.Name()
		}
	}
	return fallback
}
