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
// parks the job in error for the user.
type Job struct {
	ID            int64
	LibraryItemID int64
	DestinationID int64
	Kind          JobKind
	Order         int
	Status        JobStatus
	Phase         string
	BytesTotal    int64
	BytesDone     int64
	Error         string
	Attempts      int
	CreatedAt     string
	UpdatedAt     string
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

// Destination is one transfer target (spec §8). Filesystem holds the
// effective value (detected, or the user's override choice); FSOverride
// records an explicit user choice ("fat32"/"exfat"/""), empty meaning none.
type Destination struct {
	ID         int64
	Path       string
	Kind       DestinationKind
	Filesystem string
	FSOverride string
	BDMPrefix  string
	FreeBytes  int64
	TotalBytes int64
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
