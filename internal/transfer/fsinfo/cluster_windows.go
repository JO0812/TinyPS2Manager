//go:build windows

package fsinfo

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ClusterSize reports the filesystem allocation unit (sectors per cluster
// × bytes per sector) for path's volume, for the spec §2.11
// default-allocation-unit heuristic.
func ClusterSize(path string) (int64, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return -1, err
	}
	vol := filepath.VolumeName(abs)
	if vol == "" {
		return -1, errNoVolume
	}
	rootPtr, err := windows.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return -1, err
	}
	proc := windows.MustLoadDLL("kernel32.dll").MustFindProc("GetDiskFreeSpaceW")
	var spc, bps uint32
	r1, _, _ := proc.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&spc)),
		uintptr(unsafe.Pointer(&bps)),
		0, 0,
	)
	if r1 == 0 || spc == 0 || bps == 0 {
		return -1, errProbeSpace
	}
	return int64(spc) * int64(bps), nil
}
