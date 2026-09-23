// Package queue implements the job model, SQLite persistence, and the
// sequential transfer executor (spec §6.4). Exactly one destination-writing
// operation may be in flight per destination device at any time.
//
// Job kinds: copy, split-and-copy, convert-and-copy. Conversion/splitting
// runs into a staging area and may overlap a different job's destination
// write (prep pipelining); the final move/copy onto the destination is always
// serialized. State is persisted so an interrupted run resumes on relaunch.
//
// The sequential executor is literal against spec §6.4 requirements #1–#9;
// see BUILD-PLAN.md §4.2 for the implementation mapping.
package queue