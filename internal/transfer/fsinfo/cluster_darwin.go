//go:build darwin

package fsinfo

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ClusterSize reports the filesystem allocation unit (statfs block size)
// for path's mount, for the spec §2.11 default-allocation-unit heuristic.
func ClusterSize(path string) (int64, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return -1, err
	}
	var st unix.Statfs_t
	if err := unix.Statfs(abs, &st); err != nil {
		return -1, err
	}
	return int64(st.Bsize), nil
}
