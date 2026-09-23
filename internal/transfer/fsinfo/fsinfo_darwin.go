//go:build darwin

package fsinfo

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Probe returns the raw fstype of path's mount, free bytes, and total
// bytes (-1 each when unknowable).
func Probe(path string) (string, int64, int64, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", -1, -1, err
	}
	var st unix.Statfs_t
	if err := unix.Statfs(abs, &st); err != nil {
		return "", -1, -1, err
	}
	return fstypename(st.Fstypename[:]), int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize), nil
}

// fstypename decodes the NUL-terminated fstype name. "msdos" covers
// FAT12/16/32; the caller maps it to fat32 (documented assumption —
// FAT12/16 sticks are rare and the user toggle corrects).
func fstypename(raw []byte) string {
	var b []byte
	for _, c := range raw {
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}
