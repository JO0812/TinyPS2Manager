package queue

// JobKind is the transfer operation (spec §6.4 #3, extended with the §2.10
// Ember kind; ember and enrich execute in their own milestones and report
// not-implemented until then).
type JobKind string

const (
	KindCopy         JobKind = "copy"
	KindSplitAndCopy JobKind = "split-and-copy"
	KindConvertCopy  JobKind = "convert-and-copy"
	KindEmberCopy    JobKind = "copy-ps1-ember"
	KindEnrich       JobKind = "enrich"
)

// JobStatus is the lifecycle state (spec §8).
type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobPaused  JobStatus = "paused"
	JobError   JobStatus = "error"
	JobDone    JobStatus = "done"
)

// Job phases surface in the UI progress bar (spec §7).
const (
	PhaseConverting = "Converting"
	PhaseSplitting  = "Splitting"
	PhaseCopying    = "Copying"
	PhaseVerifying  = "Verifying"
	PhaseDone       = "Done"
)

// Job is one queued transfer (spec §8). Attempts counts failed tries;
// the executor auto-retries write-phase failures up to MaxAttempts, then
// parks the job in error for the user. The destination is a path, not an
// id, so jobs survive replugs and need no registry row.
type Job struct {
	ID              int64
	LibraryItemID   int64
	DestinationPath string
	Kind            JobKind
	Order           int
	Status          JobStatus
	Phase           string
	BytesTotal      int64
	BytesDone       int64
	Error           string
	Attempts        int
	CreatedAt       string
	UpdatedAt       string
}

// MaxAttempts caps automatic retries (plan §6.2); manual Retry resets the
// counter and always works.
const MaxAttempts = 3

// DestinationKind distinguishes live drives from staging folders.
type DestinationKind string

const (
	DestDrive  DestinationKind = "drive"
	DestFolder DestinationKind = "folder"
)

// DestinationSettings is the persisted per-path customization (kind,
// prefix, override). Everything else about a destination is resolved live.
type DestinationSettings struct {
	Path       string
	Kind       DestinationKind
	FSOverride string
	BDMPrefix  string
	UpdatedAt  string
}

// Destination is one resolved transfer target (spec §8): user settings
// merged with live detection. Filesystem holds the detected value;
// FSOverride records an explicit user choice ("fat32"/"exfat"/""),
// empty meaning none. Customized reports a stored settings row. Label is
// the volume label for drives, "" otherwise.
type Destination struct {
	Path       string
	Label      string
	Kind       DestinationKind
	Filesystem string
	FSOverride string
	BDMPrefix  string
	FreeBytes  int64
	TotalBytes int64
	Customized bool
	UpdatedAt  string
}

// EffectiveFilesystem resolves the filesystem the splitting logic must use:
// explicit override wins, else detection, else unknown.
func (d *Destination) EffectiveFilesystem() string {
	if d.FSOverride != "" {
		return d.FSOverride
	}
	return d.Filesystem
}
