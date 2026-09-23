package library

// Platform is the source console family.
type Platform string

const (
	PlatformPS2 Platform = "ps2"
	PlatformPS1 Platform = "ps1"
)

// DiscType is the OPL destination bucket for a PS2 title (spec §2.3).
// PS1 titles leave it empty.
type DiscType string

const (
	DiscCD  DiscType = "cd"
	DiscDVD DiscType = "dvd"
)

// DetectionMethod records which §2.3 strategy step produced the disc type.
type DetectionMethod string

const (
	MethodOverride  DetectionMethod = "override"
	MethodInspected DetectionMethod = "inspected"
	MethodHeuristic DetectionMethod = "heuristic"
	MethodDatabase  DetectionMethod = "database"
)

// Status is the library lifecycle state (spec §8).
type Status string

const (
	StatusNew    Status = "new"
	StatusQueued Status = "queued"
	StatusDone   Status = "done"
	StatusError  Status = "error"
)

// CDCapacityBytes is the heuristic split point: a title at or under this is
// assumed CD, above it DVD (spec §2.3 #3). 700 MiB matches the CD-ROM
// physical ceiling the heuristic stands in for.
const CDCapacityBytes = 700 * 1024 * 1024

// LibraryItem is one imported source title (spec §8). DiscGroupID links
// multi-disc titles; it is nil for singles.
type LibraryItem struct {
	ID              int64
	SourcePath      string
	ContentHash     string
	Platform        Platform
	DiscType        DiscType
	DetectionMethod DetectionMethod
	Title           string
	DiscIndex       int
	DiscGroupID     *int64
	SizeBytes       int64
	Status          Status
}
