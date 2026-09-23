//go:build windows

package fsinfo

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Probe returns the raw fstype of path's volume, free bytes, and total
// bytes (-1 each when unknowable).
func Probe(path string) (string, int64, int64, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", -1, -1, err
	}
	vol := filepath.VolumeName(abs)
	if vol == "" {
		return "", -1, -1, errNoVolume
	}
	root := vol + `\`
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", -1, -1, err
	}
	var fsName [32]uint16
	if err := windows.GetVolumeInformation(rootPtr, nil, 0, nil, nil, nil,
		&fsName[0], uint32(len(fsName))); err != nil {
		return "", -1, -1, err
	}
	name := windows.UTF16ToString(fsName[:])
	avail, total, err := diskSpaceBytes(abs)
	if err != nil {
		return name, -1, -1, nil // type known, space not
	}
	return name, avail, total, nil
}

// diskSpaceBytes wraps kernel32 GetDiskFreeSpaceExW (no x/sys wrapper).
func diskSpaceBytes(dir string) (avail, total int64, err error) {
	dirPtr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return -1, -1, err
	}
	proc := windows.MustLoadDLL("kernel32.dll").MustFindProc("GetDiskFreeSpaceExW")
	var availU, totalU uint64
	r1, _, _ := proc.Call(
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(unsafe.Pointer(&availU)),
		uintptr(unsafe.Pointer(&totalU)),
		0,
	)
	if r1 == 0 {
		return -1, -1, errProbeSpace
	}
	return int64(availU), int64(totalU), nil
}

var errProbeSpace = errorString("free space unavailable")

var errNoVolume = errorString("no volume in path")

type errorString string

func (e errorString) Error() string { return string(e) }
