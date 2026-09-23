// Package fsinfo detects destination filesystem type and free space per OS
// (spec §6.3). Implementations are build-tagged:
//   - fsinfo_linux.go   — parse /proc/mounts + unix.Statfs
//   - fsinfo_darwin.go  — getmntinfo via unix.Statfs + diskutil plist fallback
//   - fsinfo_windows.go — GetVolumeInformationW via golang.org/x/sys/windows
//
// A shared stub returns "unknown" for any OS not matched so the caller can
// fall back to a user-supplied FAT32/exFAT override (spec §6.3).
package fsinfo