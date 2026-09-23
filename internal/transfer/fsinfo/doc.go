// Package fsinfo detects the on-disk filesystem type and free space for a
// path, one build-tagged implementation per OS. All results flow through
// Probe, which returns the raw fstype name ("vfat", "exfat", "msdos",
// "FAT32", …) and free bytes (-1 when unknowable); the caller maps names to
// transfer.Filesystem. Detection is best-effort by design: unknown results
// route to the explicit user override (spec §6.3), never to a guess.
package fsinfo