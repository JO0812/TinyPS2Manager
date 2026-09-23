//go:build !linux && !darwin && !windows

package fsinfo

import "errors"

// Probe is unsupported on this platform: detection unavailable, so callers
// must use the explicit user override (spec §6.3).
func Probe(path string) (fstype string, freeBytes int64, err error) {
	_ = path
	return "", -1, errors.New("filesystem detection unsupported on this platform")
}
