// Package queue implements the job model, SQLite persistence, and the
// sequential transfer executor (spec §6.4).
//
// Execution model (one writer goroutine per destination):
//
//   - Exactly one destination-writing operation is in flight per device
//     because exactly one goroutine performs ALL destination writes for
//     that device. The invariant is structural, not advisory, and the
//     fake-Disk tests assert non-overlapping write windows.
//   - Prep (plan computation: cue parsing, serial reads, manifest builds)
//     is CPU-bound and runs one job ahead in the background while the
//     current job writes. Prep never materializes bytes, so a discarded
//     preparation (reorder/cancel) leaks nothing. Deliberately, conversion
//     OUTPUT streams straight to a same-volume temp + rename (spec §6.4
//     #5) instead of a staging copy: two concurrent destination writers
//     would violate #1, so overlap is CPU-prep over IO-write by design.
//   - Cancel lands between jobs/phases and inside sized streams; partial
//     outputs are deleted and the job returns to pending.
//   - Crash recovery is explicit, not clever: on start the executor resets
//     jobs stuck in running back to pending, sweeps orphaned .oplbm.*
//     temps, and re-runs from scratch. Checkpoints record progress for the
//     UI and crash detection; they are not byte offsets.
package queue
