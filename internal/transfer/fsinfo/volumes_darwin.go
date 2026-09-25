//go:build darwin

package fsinfo

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Volumes lists mounted volumes under /Volumes (pure Go, no cgo, no
// shelling to diskutil). macOS offers no cgo-free way to distinguish
// external from internal volumes (that needs DiskArbitration), so every
// entry is listed with Removable=false rather than guessed — the UI shows
// them all and the user picks.
func Volumes() ([]Volume, error) {
	entries, err := os.ReadDir("/Volumes")
	if err != nil {
		return nil, err
	}
	var out []Volume
	for _, e := range entries {
		mp := filepath.Join("/Volumes", e.Name())
		v := Volume{
			Path:       mp,
			Label:      e.Name(),
			Filesystem: "",
			FreeBytes:  -1,
			TotalBytes: -1,
			Removable:  false,
		}
		var st unix.Statfs_t
		if err := unix.Statfs(mp, &st); err == nil {
			v.Filesystem = fstypename(st.Fstypename[:])
			v.FreeBytes = int64(st.Bavail) * int64(st.Bsize)
			v.TotalBytes = int64(st.Blocks) * int64(st.Bsize)
		}
		out = append(out, v)
	}
	return out, nil
}
