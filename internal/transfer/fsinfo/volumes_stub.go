//go:build !linux && !darwin && !windows

package fsinfo

// Volumes is unsupported on this platform: no drive candidates are
// reported and callers fall back to the manual path form (spec §6.3).
func Volumes() ([]Volume, error) {
	return nil, nil
}
