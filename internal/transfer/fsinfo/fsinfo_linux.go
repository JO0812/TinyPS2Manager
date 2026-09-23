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

// Probe returns the raw fstype of path's mount and its free bytes.
func Probe(path string) (fstype string, freeBytes int64, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", -1, err
	}
	mount, fstype, err := mountFor(abs)
	if err != nil {
		return "", -1, err
	}
	_ = mount
	var st unix.Statfs_t
	if err := unix.Statfs(abs, &st); err != nil {
		return fstype, -1, nil // type known, space not
	}
	return fstype, int64(st.Bavail) * int64(st.Bsize), nil
}

// mountFor finds the longest-prefix mount point of path in /proc/mounts.
func mountFor(path string) (mount, fstype string, err error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	return parseMounts(f, path)
}

// parseMounts scans mounts-format lines for the deepest mount containing
// path. Boundary-aware: "/mnt" does not match "/mnt2".
func parseMounts(r io.Reader, path string) (mount, fstype string, err error) {
	best, bestType := "", ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		mp, ft := fields[1], fields[2]
		if mp == "/" || path == mp || strings.HasPrefix(path, mp+"/") {
			if len(mp) > len(best) {
				best, bestType = mp, ft
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	if best == "" {
		return "", "", io.ErrUnexpectedEOF
	}
	return best, bestType, nil
}
