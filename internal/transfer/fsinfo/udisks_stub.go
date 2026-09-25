//go:build !linux

package fsinfo

import "errors"

// UdisksLookup is a Linux-only UDisks2 query; other platforms always fall
// back to their native detection paths.
func UdisksLookup(partDev string) (UdisksInfo, error) {
	return UdisksInfo{}, errors.New("udisks2 unavailable on this platform")
}
