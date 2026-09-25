//go:build windows

package fsinfo

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Volumes lists logical drives that can hold games: removable and fixed
// drives (USB HDDs usually report as fixed). Optical and network drives
// are skipped. Pure Go via x/sys/windows, no shelling.
func Volumes() ([]Volume, error) {
	// GetLogicalDrives bitmask: bit n => drive 'A'+n present.
	var mask uint32
	proc := windows.MustLoadDLL("kernel32.dll").MustFindProc("GetLogicalDrives")
	r1, _, _ := proc.Call()
	mask = uint32(r1)
	var out []Volume
	for i := uint(0); i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		dtProc := windows.MustLoadDLL("kernel32.dll").MustFindProc("GetDriveTypeW")
		rootPtr, _ := windows.UTF16PtrFromString(root)
		dt, _, _ := dtProc.Call(uintptr(unsafe.Pointer(rootPtr)))
		// 2 = removable, 3 = fixed; skip unknown/remote/cdrom/RAM disk.
		if dt != 2 && dt != 3 {
			continue
		}
		v := Volume{
			Path:       root,
			Label:      filepath.VolumeName(root),
			Filesystem: "",
			FreeBytes:  -1,
			TotalBytes: -1,
			Removable:  dt == 2,
		}
		var fsName [32]uint16
		var volName [256]uint16
		if err := windows.GetVolumeInformation(rootPtr, &volName[0], uint32(len(volName)),
			nil, nil, nil, &fsName[0], uint32(len(fsName))); err == nil {
			if label := windows.UTF16ToString(volName[:]); label != "" {
				v.Label = label
			}
			v.Filesystem = windows.UTF16ToString(fsName[:])
		}
		if avail, total, err := diskSpaceBytes(root); err == nil {
			v.FreeBytes, v.TotalBytes = avail, total
		}
		out = append(out, v)
	}
	return out, nil
}
