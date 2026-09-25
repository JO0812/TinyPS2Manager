package fsinfo

// UdisksInfo carries UDisks2 facts about one partition device: its
// partition-table type, its own partition type byte, and the drive's
// rotation rate. Empty strings / HasRotation=false mean "unknown".
type UdisksInfo struct {
	Table        string // dos, gpt, or "" when unknown
	PartType     string // 0x0c etc, or "" when unknown
	RotationRate int32  // <0 = non-rotating (flash); >=0 = RPM or unknown
	HasRotation  bool
}

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
