//go:build linux

package fsinfo

import (
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// UdisksLookup queries UDisks2 over the system bus for partDev (e.g.
// /dev/sdb1) without root and without shelling. It returns an error when
// UDisks2 is unreachable or the device is unknown — callers fall back to
// direct block reads (which need privileges) and finally to warnings.
func UdisksLookup(partDev string) (UdisksInfo, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return UdisksInfo{}, err
	}
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	obj := conn.Object("org.freedesktop.UDisks2", "/org/freedesktop/UDisks2")
	if err := obj.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return UdisksInfo{}, err
	}
	var info UdisksInfo
	var tablePath, drivePath dbus.ObjectPath
	found := false
	for _, ifaces := range objs {
		block, ok := ifaces["org.freedesktop.UDisks2.Block"]
		if !ok {
			continue
		}
		if deviceString(block["Device"]) != partDev {
			continue
		}
		found = true
		if part, ok := ifaces["org.freedesktop.UDisks2.Partition"]; ok {
			info.PartType = variantString(part["Type"])
			tablePath, _ = part["Table"].Value().(dbus.ObjectPath)
		}
		if drive, ok := block["Drive"]; ok {
			if p, ok := drive.Value().(dbus.ObjectPath); ok {
				drivePath = p
			}
		}
		break
	}
	if !found {
		return UdisksInfo{}, fmt.Errorf("udisks2: no block device for %s", partDev)
	}
	if tablePath != "" {
		if ifaces, ok := objs[tablePath]; ok {
			if table, ok := ifaces["org.freedesktop.UDisks2.PartitionTable"]; ok {
				info.Table = strings.ToLower(variantString(table["Type"]))
			}
		}
	}
	if drivePath != "" {
		if ifaces, ok := objs[drivePath]; ok {
			if drive, ok := ifaces["org.freedesktop.UDisks2.Drive"]; ok {
				if v, ok := drive["RotationRate"]; ok {
					if rate, ok := v.Value().(int32); ok {
						info.RotationRate, info.HasRotation = rate, true
					}
				}
			}
		}
	}
	return info, nil
}

func deviceString(v dbus.Variant) string {
	if v.Signature().String() != "ay" {
		return ""
	}
	if b, ok := v.Value().([]byte); ok {
		return strings.TrimRight(string(b), "\x00")
	}
	return ""
}

func variantString(v dbus.Variant) string {
	if s, ok := v.Value().(string); ok {
		return s
	}
	return ""
}
