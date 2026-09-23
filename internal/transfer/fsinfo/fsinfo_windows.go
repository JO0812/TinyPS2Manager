//go:build windows

package fsinfo

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Probe returns the raw fstype of path's volume and its free bytes.
func Probe(path string) (fstype string, freeBytes int64, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", -1, err
	}
	vol := filepath.VolumeName(abs)
	if vol == "" {
		return "", -1, errNoVolume
	}
	root := vol + `\`
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", -1, err
	}
	var fsName [32]uint16
	if err := windows.GetVolumeInformation(rootPtr, nil, 0, nil, nil, nil,
		&fsName[0], uint32(len(fsName))); err != nil {
		return "", -1, err
	}
	name := windows.UTF16ToString(fsName[:])
	avail, err := diskFreeBytes(abs)
	if err != nil {
		return name, -1, nil // type known, space not
	}
	return name, avail, nil
}

// diskFreeBytes wraps kernel32 GetDiskFreeSpaceExW (no x/sys wrapper).
func diskFreeBytes(dir string) (int64, error) {
	dirPtr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return -1, err
	}
	proc := windows.MustLoadDLL("kernel32.dll").MustFindProc("GetDiskFreeSpaceExW")
	var avail uint64
	r1, _, _ := proc.Call(
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(unsafe.Pointer(&avail)),
		0,
		0,
	)
	if r1 == 0 {
		return -1, errProbeSpace
	}
	return int64(avail), nil
}

var errProbeSpace = errorString("free space unavailable")

var errNoVolume = errorString("no volume in path")

type errorString string

func (e errorString) Error() string { return string(e) }
