// Package transfer abstracts the destination (local drive or folder) and
// provides free-space checks, filesystem-type detection (FAT32 vs exFAT per
// spec §6.3), atomic write-then-rename, and post-copy verification.
//
// Per spec §10 Q2 the app writes directly to mounted USB/SD drives, so a
// real drive implementation ships in M2 (not staging-folder only). A per-OS
// fsinfo subpackage detects the filesystem type; on failure or for "folder
// for later imaging" destinations, the caller falls back to the explicit
// FAT32/exFAT override defaulting to FAT32 (safer, more restrictive).
//
// The drive/folder distinction is metadata only (queue.Destination.Kind):
// both are paths and detect, check, and write identically, so there is one
// implementation, not two. Contiguity (spec §6.4 #5) comes from FileDisk:
// the final bytes stream into a temp file on the SAME volume and rename
// into place, so each file lands in a single allocation extent instead of
// being copied twice across volumes.
package transfer
