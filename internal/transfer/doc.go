// Package transfer abstracts the destination (local drive or folder) and
// provides free-space checks, filesystem-type detection (FAT32 vs exFAT per
// spec §6.3), atomic write-then-rename, and post-copy verification.
//
// Per spec §10 Q2 the app writes directly to mounted USB/SD drives, so a real
// drive implementation ships in M2 (not staging-folder only). A per-OS fsinfo
// subpackage detects the filesystem type; on failure or for "folder for later
// imaging" destinations, the UI offers an explicit FAT32/exFAT override
// defaulting to FAT32 (safer, more restrictive).
package transfer