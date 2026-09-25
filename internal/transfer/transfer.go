package transfer

import (
	"strings"

	"github.com/jo/TinyPS2Manager/internal/transfer/fsinfo"
)

// Filesystem is the effective on-disk filesystem driving split decisions.
type Filesystem string

const (
	FSFAT32   Filesystem = "fat32"
	FSExFAT   Filesystem = "exfat"
	FSUnknown Filesystem = "unknown"
)

// Probed is a live destination reading: effective filesystem, free bytes,
// and total bytes (-1 each when unknowable — callers skip the space check
// then, never block on it).
type Probed struct {
	Filesystem Filesystem
	FreeBytes  int64
	TotalBytes int64
}

// Probe detects path's filesystem, free space, and capacity. Raw names map
// case-insensitively; "msdos" (FAT12/16/32) folds to fat32 as a documented
// approximation — FAT12/16 sticks are rare and the explicit user override
// corrects any misfire.
func Probe(path string) (Probed, error) {
	raw, free, total, err := fsinfo.Probe(path)
	if err != nil {
		return Probed{Filesystem: FSUnknown, FreeBytes: -1, TotalBytes: -1}, nil
	}
	return Probed{Filesystem: mapFilesystem(raw), FreeBytes: free, TotalBytes: total}, nil
}

func mapFilesystem(raw string) Filesystem {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "vfat", "fat32", "msdos", "fat":
		return FSFAT32
	case "exfat":
		return FSExFAT
	default:
		return FSUnknown
	}
}

// Volume is one detected external-drive candidate for the destination
// picker: the user selects it instead of typing a path.
type Volume struct {
	Path       string
	Label      string
	Filesystem Filesystem
	FreeBytes  int64
	TotalBytes int64
	Removable  bool
}

// Volumes lists detected external drives (fsinfo per OS). Empty (not an
// error) on platforms without detection — callers keep the manual path
// form as fallback.
func Volumes() ([]Volume, error) {
	raw, err := fsinfo.Volumes()
	if err != nil {
		return nil, err
	}
	out := make([]Volume, 0, len(raw))
	for _, v := range raw {
		out = append(out, Volume{
			Path:       v.Path,
			Label:      v.Label,
			Filesystem: mapFilesystem(v.Filesystem),
			FreeBytes:  v.FreeBytes,
			TotalBytes: v.TotalBytes,
			Removable:  v.Removable,
		})
	}
	return out, nil
}
