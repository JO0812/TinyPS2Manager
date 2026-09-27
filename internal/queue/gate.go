package queue

import "sync"

// destGates serializes all writes to a destination path process-wide (spec
// §2.6/§2.8: game writes run one at a time per volume, and enrich-class
// staging never interleaves with a game write). The executor holds the gate
// for the whole write+verify phase of each job; the synchronous enrich API
// handlers hold it around their staging writes, so enrich output always
// lands strictly between game jobs.
var (
	gateMu sync.Mutex
	gates  = map[string]*sync.Mutex{}
)

// LockDestination acquires the exclusive write gate for path and returns the
// unlock func. Paths are used verbatim; callers pass the resolved,
// absolute destination path.
func LockDestination(path string) func() {
	gateMu.Lock()
	m, ok := gates[path]
	if !ok {
		m = &sync.Mutex{}
		gates[path] = m
	}
	gateMu.Unlock()
	m.Lock()
	return m.Unlock
}
