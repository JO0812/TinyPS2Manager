//go:build !linux && !darwin && !windows

package fsinfo

import "errors"

// ClusterSize is unsupported on this platform: callers warn instead of
// checking the spec §2.11 default-allocation-unit heuristic.
func ClusterSize(path string) (int64, error) {
	_ = path
	return -1, errors.New("cluster size detection unsupported on this platform")
}
