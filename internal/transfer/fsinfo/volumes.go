package fsinfo

// Volume is one detected external/removable drive candidate: a mounted
// filesystem the user can pick as a destination without typing a path.
// Removable is best-effort per OS (Linux knows via udisks mount points;
// other platforms list all plausible volumes and mark them non-removable
// rather than guessing).
type Volume struct {
	Path       string
	Label      string
	Filesystem string // raw fstype, e.g. vfat, exfat, msdos
	FreeBytes  int64  // -1 when unknowable
	TotalBytes int64  // -1 when unknowable
	Removable  bool
}
