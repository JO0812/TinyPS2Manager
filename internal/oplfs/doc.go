// Package oplfs builds and validates the OPL folder tree (spec §2.1, §2.5):
// DVD/, CD/, POPS/ with case-sensitive names and optional BDM prefix.
//
// It also generates and validates the per-disc VMC artifacts required for
// PS1 multi-disc support (spec §2.5):
//   - DISCS.TXT: ≤4 lines, each ≤73 chars (POPSLoader practical limit), identical in every disc folder.
//   - VMCDIR.TXT: single line, ≤103 bytes, no '/ \ :' chars, bare folder name resolving inside POPS/.
//
// Validation runs at enqueue time (non-functional invariant N4 in BUILD-PLAN.md)
// so violations surface before any destination write begins.
package oplfs
