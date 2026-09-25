package fsinfo

import (
	"strings"
	"testing"
)

func TestRemovableDevice(t *testing.T) {
	for _, dev := range []string{
		"/dev/sda", "/dev/sda1", "/dev/sdb1", "/dev/sdz9",
		"/dev/mmcblk0", "/dev/mmcblk0p1",
		"/dev/nvme0n1", "/dev/nvme0n1p1",
		"/dev/vda1", "/dev/hda1",
	} {
		if !removableDevice(dev) {
			t.Errorf("%s should qualify", dev)
		}
	}
	for _, dev := range []string{
		"/dev/loop0", "/dev/dm-0", "/dev/md0", "overlay", "tmpfs",
		"/dev/sd", "/dev/zram0", "/dev/sr0",
	} {
		if removableDevice(dev) {
			t.Errorf("%s should not qualify", dev)
		}
	}
}

func TestVolumesFromMounts(t *testing.T) {
	mounts := `overlay / overlay rw,relatime 0 0
/dev/sda1 /media/jo/34FF-D9F8 exfat rw,relatime 0 0
/dev/sdb1 /mnt/stick vfat rw 0 0
/dev/sdb1 /mnt/stick vfat rw 0 0
/dev/sdc1 /home/jo/data ext4 rw 0 0
/dev/loop0 /snap/foo snap squashfs ro 0 0
tmpfs /tmp tmpfs rw 0 0
`
	vols, err := volumesFromMounts(strings.NewReader(mounts))
	if err != nil {
		t.Fatalf("volumesFromMounts: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("volumes = %+v, want 2", vols)
	}
	if vols[0].Path != "/media/jo/34FF-D9F8" || vols[0].Filesystem != "exfat" {
		t.Errorf("vol0 = %+v", vols[0])
	}
	if !vols[0].Removable {
		t.Errorf("vol0 should be removable: %+v", vols[0])
	}
	if vols[1].Path != "/mnt/stick" || vols[1].Removable {
		t.Errorf("vol1 = %+v (want non-removable /mnt entry, deduped)", vols[1])
	}
	// Label falls back to the mount basename when /dev/disk/by-label
	// has no matching entry (fake devices never match).
	if vols[0].Label != "34FF-D9F8" {
		t.Errorf("label = %q, want mount basename", vols[0].Label)
	}
}

func TestVolumesLive(t *testing.T) {
	vols, err := Volumes()
	if err != nil {
		t.Fatalf("Volumes: %v", err)
	}
	for _, v := range vols {
		if v.Path == "" {
			t.Errorf("volume with empty path: %+v", v)
		}
		t.Logf("volume path=%q label=%q fs=%q free=%d total=%d removable=%v",
			v.Path, v.Label, v.Filesystem, v.FreeBytes, v.TotalBytes, v.Removable)
	}
}
