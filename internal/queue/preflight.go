package queue

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/transfer/fsinfo"
	"github.com/jo/TinyPS2Manager/internal/usbextreme"
)

// Check status constants.
const (
	CheckPass = "pass"
	CheckFail = "fail"
	CheckWarn = "warn"
)

// Check is one pre-flight row (spec §2.11).
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // pass/fail/warn
	Message string `json:"message"`
}

// Result is the pre-flight report. Blocked is true if any fail check
// mandates blocking enqueue (partition table, fs mismatch, ul.cfg corrupt,
// free-space probe).
type Result struct {
	Checks  []Check `json:"checks"`
	Blocked bool    `json:"blocked"`
}

// Preflight validates destination preparation (spec §2.11). It is pure Go,
// no shelling, no cgo, and never prompts for sudo. Checks that fail block
// enqueue; warnings do not.
//
// Blocking (fail-closed):
// - partition table is dos (MBR) — GPT is unsupported; folder destinations skip with warn
// - partition type vs fs toggle (when detectable)
// - partition start is 1 MiB-aligned (drives only)
// - filesystem matches toggle (via fsinfo)
// - ul.cfg+ul.* sets parse (corrupt → fail)
// - free-space probe (≥1 MiB contiguous)
// Warnings (non-blocking):
// - non-flash device hints (rotational/SD-adapter heuristics where detectable)
// - exFAT non-default cluster size (heuristic via Statfs bsize)
// - pre-existing fragmentation risk (>100 files, suggest re-copy)
func Preflight(dest *Destination) (*Result, error) {
	if dest == nil {
		return nil, fmt.Errorf("no destination")
	}
	var checks []Check
	add := func(name, status, msg string) {
		checks = append(checks, Check{Name: name, Status: status, Message: msg})
	}
	blocked := false
	markBlocked := func() { blocked = true }

	// 1. Filesystem matches toggle
	fstype, freeBytes, totalBytes, err := fsinfo.Probe(dest.Path)
	if err != nil {
		add("filesystem", CheckWarn, fmt.Sprintf("filesystem probe failed: %v (set explicit FAT32/exFAT toggle)", err))
	} else {
		detected := normalizeFSType(fstype)
		eff := strings.ToLower(dest.EffectiveFilesystem())
		status, msg := fsToggleVerdict(dest.Kind, detected, eff)
		add("filesystem", status, msg)
		if status == CheckFail {
			markBlocked()
		}
		// 2. Free-space probe
		if freeBytes >= 0 {
			if freeBytes < 1<<20 {
				add("free-space", CheckFail, fmt.Sprintf("free space %d bytes < 1 MiB probe", freeBytes))
				markBlocked()
			} else {
				add("free-space", CheckPass, fmt.Sprintf("%d bytes free", freeBytes))
			}
		} else {
			add("free-space", CheckWarn, "free space unknowable")
		}
	}

	// 3. ul.cfg+ul.* pre-existing sets parse
	base := filepath.Join(dest.Path, dest.BDMPrefix)
	if entries, err := usbextreme.List(base); err != nil {
		add("ul.cfg", CheckFail, fmt.Sprintf("ul.cfg corrupt: %v", err))
		markBlocked()
	} else if len(entries) > 0 {
		failed := false
		for _, e := range entries {
			if err := usbextreme.Verify(base, e.Serial); err != nil {
				add("ul.cfg", CheckFail, fmt.Sprintf("ul set %s corrupt: %v", e.Serial, err))
				markBlocked()
				failed = true
				break
			}
		}
		if !failed {
			add("ul.cfg", CheckPass, fmt.Sprintf("%d existing ul set(s) parse OK", len(entries)))
		}
	} else {
		add("ul.cfg", CheckPass, "no existing ul sets")
	}

	// 4. Partition table is dos (MBR) — folder destinations skip with warn
	table, tableMsg, _ := probePartitionTable(dest.Path, dest.Kind)
	if table == "dos" {
		add("partition-table", CheckPass, "MBR (dos) partition table")
	} else if table == "gpt" {
		add("partition-table", CheckFail, "partition table is GPT, must be MBR (dos) — repartition with MBR")
		markBlocked()
	} else {
		// unknown or skipped: warn, not block (folder destinations)
		if dest.Kind == DestFolder {
			add("partition-table", CheckWarn, tableMsg)
		} else {
			add("partition-table", CheckWarn, tableMsg)
		}
	}

	// 5. Partition type vs fs toggle (when detectable)
	if dest.Kind == DestDrive {
		ptype, pmsg, _ := probePartitionType(dest.Path)
		if ptype != "" {
			eff := strings.ToLower(dest.EffectiveFilesystem())
			expected := ""
			switch eff {
			case "fat32":
				expected = "0x0c"
			case "exfat":
				expected = "0x07"
			}
			if expected != "" && !strings.EqualFold(ptype, expected) && ptype != "0x0b" && ptype != "0x0c" && ptype != "0x07" {
				add("partition-type", CheckFail, fmt.Sprintf("partition type %s vs fs %s mismatch (expected %s)", ptype, eff, expected))
				markBlocked()
			} else {
				add("partition-type", CheckPass, fmt.Sprintf("partition type %s matches %s", ptype, eff))
			}
		} else {
			add("partition-type", CheckWarn, pmsg)
		}
	} else {
		add("partition-type", CheckWarn, "folder destination: partition type check skipped (only for drives)")
	}

	// 6. Partition start alignment (spec §2.11): the partition must start
	// 1 MiB-aligned (sector 2048); legacy offsets mount nowhere.
	if dest.Kind == DestFolder {
		add("partition-alignment", CheckWarn, "folder destination: alignment check skipped (only for drives)")
	} else if off, ok, msg := probePartitionOffset(dest.Path); !ok {
		add("partition-alignment", CheckWarn, msg)
	} else if off%(1<<20) == 0 {
		add("partition-alignment", CheckPass, fmt.Sprintf("partition starts at byte %d (1 MiB-aligned)", off))
	} else {
		add("partition-alignment", CheckFail, fmt.Sprintf("partition starts at byte %d (sector %d), not 1 MiB-aligned — repartition starting at sector 2048 or later", off, off/512))
		markBlocked()
	}

	// 7. exFAT allocation unit heuristic (spec §2.11: default AUS required).
	// Warn-only: the measured unit is a statfs-level heuristic.
	if strings.ToLower(dest.EffectiveFilesystem()) == "exfat" && dest.Kind == DestDrive {
		want := defaultExFATCluster(totalBytes)
		got, cerr := fsinfo.ClusterSize(dest.Path)
		switch {
		case want <= 0 || totalBytes <= 0:
			add("cluster-size", CheckWarn, "exFAT cluster check skipped: volume size unknowable")
		case cerr != nil:
			add("cluster-size", CheckWarn, fmt.Sprintf("exFAT cluster size unknowable: %v", cerr))
		case got != want:
			add("cluster-size", CheckWarn, fmt.Sprintf("exFAT allocation unit is %d bytes, default for this size is %d — reformat with defaults", got, want))
		default:
			add("cluster-size", CheckPass, fmt.Sprintf("exFAT allocation unit %d bytes (default)", got))
		}
	}

	// Warnings: non-flash hints, fragmentation risk
	if hint, msg := probeNonFlash(dest.Path); hint {
		add("device-type", CheckWarn, msg)
	} else {
		add("device-type", CheckPass, "device appears to be flash (or undetectable)")
	}
	if n, err := countFiles(base); err == nil && n > 100 {
		add("fragmentation", CheckWarn, fmt.Sprintf("%d files in destination: fragmentation risk — consider re-copy in one sequential batch (spec §2.6)", n))
	} else if err == nil {
		add("fragmentation", CheckPass, fmt.Sprintf("%d files: low fragmentation risk", n))
	} else {
		add("fragmentation", CheckWarn, fmt.Sprintf("could not count files: %v", err))
	}

	return &Result{Checks: checks, Blocked: blocked}, nil
}

// fsToggleVerdict compares the detected host filesystem against the
// effective (toggle) filesystem. Drives are fail-closed on mismatch: the
// toggle must describe the live volume. Folders stage content for a future
// target (spec §2.11/Goal 5: "a local folder the user will image later"),
// so a host-vs-toggle mismatch only warns — planning and splitting still
// honor the toggle.
func fsToggleVerdict(kind DestinationKind, detected, eff string) (string, string) {
	if eff == "" || eff == "unknown" {
		return CheckWarn, fmt.Sprintf("detected %q but effective filesystem is unknown: set FAT32/exFAT toggle", detected)
	}
	if eff == detected || detected == "unknown" {
		return CheckPass, fmt.Sprintf("filesystem %q matches toggle %q", detected, eff)
	}
	if kind == DestFolder {
		return CheckWarn, fmt.Sprintf("folder is on %q but target toggle is %q: FAT32/exFAT rules apply per toggle at plan time", detected, eff)
	}
	return CheckFail, fmt.Sprintf("filesystem is %q, expected %q (mismatch with FAT32/exFAT toggle)", detected, eff)
}

func normalizeFSType(raw string) string {
	lower := strings.ToLower(raw)
	switch lower {
	case "vfat", "fat32", "msdos", "fat12", "fat16":
		return "fat32"
	case "exfat":
		return "exfat"
	case "ntfs":
		return "ntfs"
	case "tmpfs", "overlay", "ext4", "xfs", "btrfs", "apfs", "hfs":
		return lower
	default:
		if lower == "" {
			return "unknown"
		}
		return lower
	}
}

func countFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n, nil
}

func probeNonFlash(path string) (bool, string) {
	dev, _, err := deviceForPath(path)
	if err != nil || dev == "" {
		return false, ""
	}
	disk := diskForPartition(dev)
	if disk == "" {
		return false, ""
	}
	rotational := ""
	if data, err := os.ReadFile(filepath.Join("/sys/block", disk, "queue/rotational")); err == nil {
		rotational = strings.TrimSpace(string(data))
	}
	rate, hasRot := udisksRotation(dev)
	if spinWarn(hasRot, rate, rotational) {
		return true, "device appears to be spinning HDD (recommend plain flash pendrive; PS2 ports brown out enclosures, spec §2.11)"
	}
	if data, err := os.ReadFile(filepath.Join("/sys/block", disk, "device/model")); err == nil {
		m := strings.ToUpper(strings.TrimSpace(string(data)))
		if strings.Contains(m, "SD") || strings.Contains(m, "MMC") || strings.Contains(m, "READER") {
			return true, fmt.Sprintf("device model %q suggests SD/MMC adapter (mass:/ freezes on some units, spec §2.11)", strings.TrimSpace(string(data)))
		}
	}
	return false, ""
}

// spinWarn decides the spinning-disk warning. UDisks2 rotation evidence is
// authoritative when present (USB bridges lie: flash behind a SATA bridge
// reports rotational=1); otherwise the sysfs flag stands, conservatively.
func spinWarn(hasRotation bool, rotationRate int32, sysfsRotational string) bool {
	if hasRotation && rotationRate < 0 {
		return false
	}
	return sysfsRotational == "1"
}

// udisksRotation returns the drive's UDisks2 rotation rate for dev.
// ok=false on any failure or absence — callers keep their previous
// behavior instead of guessing.
func udisksRotation(dev string) (rate int32, ok bool) {
	info, err := fsinfo.UdisksLookup(dev)
	if err != nil || !info.HasRotation {
		return 0, false
	}
	return info.RotationRate, true
}

func deviceForPath(path string) (device, mount string, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	return parseMounts(f, abs)
}

func parseMounts(r io.Reader, path string) (device, mount string, err error) {
	bestDev, bestMount := "", ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		dev, mp := fields[0], fields[1]
		if mp == "/" || path == mp || strings.HasPrefix(path, mp+"/") {
			if len(mp) > len(bestMount) {
				bestMount, bestDev = mp, dev
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	if bestMount == "" {
		return "", "", fmt.Errorf("no mount for %s", path)
	}
	return bestDev, bestMount, nil
}

func diskForPartition(dev string) string {
	base := filepath.Base(dev)
	if strings.HasPrefix(base, "mmcblk") {
		if idx := strings.Index(base, "p"); idx > 0 {
			return base[:idx]
		}
		return base
	}
	if strings.Contains(base, "nvme") {
		if idx := strings.Index(base, "p"); idx > 0 {
			return base[:idx]
		}
		return base
	}
	i := len(base) - 1
	for i >= 0 && base[i] >= '0' && base[i] <= '9' {
		i--
	}
	if i < len(base)-1 {
		return base[:i+1]
	}
	return base
}

func probePartitionTable(path string, kind DestinationKind) (string, string, error) {
	if kind == DestFolder {
		return "unknown", "folder destination: partition table check skipped (only for drives)", nil
	}
	dev, _, err := deviceForPath(path)
	if err != nil {
		return "unknown", fmt.Sprintf("partition table check skipped: %v", err), nil
	}
	// Prefer UDisks2 (no root needed); fall back to MBR reads below.
	if info, uerr := fsinfo.UdisksLookup(dev); uerr == nil && info.Table != "" {
		switch info.Table {
		case "dos":
			return "dos", "MBR (dos) partition table (via UDisks2)", nil
		case "gpt":
			return "gpt", "partition table is GPT (via UDisks2) — repartition with MBR", nil
		}
	}
	diskDev := diskForPartition(dev)
	diskPath := filepath.Join("/dev", diskDev)
	f, err := os.Open(diskPath)
	if err != nil {
		return "unknown", fmt.Sprintf("partition table check skipped: cannot open %s (%v) — assuming MBR; repartition with MBR if unsure", diskPath, err), nil
	}
	defer f.Close()
	mbr := make([]byte, 512)
	if _, err := io.ReadFull(f, mbr); err != nil {
		return "unknown", fmt.Sprintf("partition table check skipped: cannot read MBR from %s (%v)", diskPath, err), nil
	}
	if mbr[510] != 0x55 || mbr[511] != 0xAA {
		return "unknown", fmt.Sprintf("partition table check skipped: no MBR signature on %s (maybe GPT or unpartitioned)", diskPath), nil
	}
	hasEE := false
	hasOther := false
	for i := 0; i < 4; i++ {
		ptype := mbr[446+i*16+4]
		if ptype == 0xEE {
			hasEE = true
		} else if ptype != 0x00 {
			hasOther = true
		}
	}
	if hasEE && !hasOther {
		return "gpt", "partition table is GPT (protective MBR type 0xEE only)", nil
	}
	if _, err := f.Seek(512, io.SeekStart); err == nil {
		hdr := make([]byte, 8)
		if _, err := io.ReadFull(f, hdr); err == nil && string(hdr) == "EFI PART" {
			return "gpt", "partition table is GPT (EFI PART header at LBA 1)", nil
		}
	}
	return "dos", "MBR partition table detected", nil
}

// probePartitionOffset returns the partition's byte offset from the start
// of the drive (spec §2.11 alignment). UDisks2 first (no root), then the
// MBR partition entry (needs block-read privileges). ok=false with a
// human reason when neither works.
func probePartitionOffset(path string) (uint64, bool, string) {
	dev, _, err := deviceForPath(path)
	if err != nil {
		return 0, false, fmt.Sprintf("alignment check skipped: %v", err)
	}
	if info, uerr := fsinfo.UdisksLookup(dev); uerr == nil && info.HasOffset {
		return info.PartOffset, true, ""
	}
	diskDev := diskForPartition(dev)
	partNum := partitionNumber(dev)
	if partNum <= 0 || partNum > 4 {
		return 0, false, fmt.Sprintf("alignment check skipped: cannot determine partition number for %s", dev)
	}
	diskPath := filepath.Join("/dev", diskDev)
	f, err := os.Open(diskPath)
	if err != nil {
		return 0, false, fmt.Sprintf("alignment check skipped: cannot open %s (%v)", diskPath, err)
	}
	defer f.Close()
	off, err := mbrPartitionOffset(f, partNum)
	if err != nil {
		return 0, false, fmt.Sprintf("alignment check skipped: %v", err)
	}
	return off, true, ""
}

// mbrPartitionOffset parses the MBR partition entry for a 1-based primary
// partition number and returns its byte offset (start LBA × 512).
func mbrPartitionOffset(f *os.File, partNum int) (uint64, error) {
	mbr := make([]byte, 512)
	if _, err := io.ReadFull(f, mbr); err != nil {
		return 0, fmt.Errorf("cannot read MBR (%v)", err)
	}
	if mbr[510] != 0x55 || mbr[511] != 0xAA {
		return 0, fmt.Errorf("no MBR signature")
	}
	entry := mbr[446+(partNum-1)*16:]
	if entry[4] == 0x00 {
		return 0, fmt.Errorf("partition %d type 0x00 (unused)", partNum)
	}
	lba := uint64(entry[8]) | uint64(entry[9])<<8 | uint64(entry[10])<<16 | uint64(entry[11])<<24
	return lba * 512, nil
}

// defaultExFATCluster is the Microsoft default allocation unit for an
// exFAT volume of totalBytes: 4 KiB up to 256 MiB, 32 KiB up to 32 GiB,
// 128 KiB above. ≤0 when the size is unknowable.
func defaultExFATCluster(totalBytes int64) int64 {
	switch {
	case totalBytes <= 0:
		return -1
	case totalBytes <= 256<<20:
		return 4 << 10
	case totalBytes <= 32<<30:
		return 32 << 10
	default:
		return 128 << 10
	}
}

func probePartitionType(path string) (string, string, error) {
	dev, _, err := deviceForPath(path)
	if err != nil {
		return "", fmt.Sprintf("partition type check skipped: %v", err), nil
	}
	// Prefer UDisks2 (no root needed). On GPT the type is a GUID, not a
	// 0x.. byte — the table check already owns the GPT verdict, so skip
	// here instead of double-failing.
	if info, uerr := fsinfo.UdisksLookup(dev); uerr == nil {
		if info.Table == "dos" && info.PartType != "" {
			return info.PartType, "", nil
		}
		if info.Table == "gpt" {
			return "", "GPT partition table: see partition-table check", nil
		}
	}
	diskDev := diskForPartition(dev)
	diskPath := filepath.Join("/dev", diskDev)
	f, err := os.Open(diskPath)
	if err != nil {
		return "", fmt.Sprintf("partition type check skipped: cannot open %s (%v)", diskPath, err), nil
	}
	defer f.Close()
	mbr := make([]byte, 512)
	if _, err := io.ReadFull(f, mbr); err != nil {
		return "", fmt.Sprintf("partition type check skipped: cannot read MBR from %s (%v)", diskPath, err), nil
	}
	if mbr[510] != 0x55 || mbr[511] != 0xAA {
		return "", "partition type check skipped: no MBR signature", nil
	}
	partNum := partitionNumber(dev)
	if partNum <= 0 || partNum > 4 {
		return "", fmt.Sprintf("partition type check skipped: cannot determine partition number for %s", dev), nil
	}
	ptype := mbr[446+(partNum-1)*16+4]
	if ptype == 0x00 {
		return "", fmt.Sprintf("partition %d type 0x00 (unused)", partNum), nil
	}
	return fmt.Sprintf("0x%02x", ptype), "", nil
}

func partitionNumber(dev string) int {
	base := filepath.Base(dev)
	if idx := strings.LastIndex(base, "p"); idx != -1 {
		var n int
		fmt.Sscanf(base[idx+1:], "%d", &n)
		if n > 0 {
			return n
		}
	}
	i := len(base) - 1
	for i >= 0 && base[i] >= '0' && base[i] <= '9' {
		i--
	}
	if i < len(base)-1 {
		var n int
		fmt.Sscanf(base[i+1:], "%d", &n)
		return n
	}
	return 0
}
