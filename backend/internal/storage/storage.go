// Package storage builds the read-only disk, partition and mount report for
// GET /storage (W1.3). It reads sysfs, the udev database and mountinfo only,
// so it never spins up a sleeping disk.
package storage

import (
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"nas-revival/backend/internal/hal"
)

// Disk roles derived from what the disk's partitions hold.
const (
	RoleSystem = "system" // root filesystem
	RoleBoot   = "boot"
	RoleSwap   = "swap"
	RoleData   = "data" // a filesystem under /srv
)

type Report struct {
	Disks []Disk `json:"disks"`
}

type Disk struct {
	// ID is stable across sdX renumbering (by-id name, WWN preferred). IDStable
	// is false only when udev gave no identity and the kernel name is used.
	ID             string      `json:"id"`
	IDStable       bool        `json:"idStable"`
	Name           string      `json:"name"`
	Model          *string     `json:"model"`
	Vendor         *string     `json:"vendor"`
	Serial         *string     `json:"serial"`
	Firmware       *string     `json:"firmware"`
	Bus            string      `json:"bus"`  // ata, usb, nvme, mmc, unknown
	Type           string      `json:"type"` // hdd, sshd, ssd, flash, unknown
	RotationRPM    *int        `json:"rotationRpm"`
	SizeBytes      uint64      `json:"sizeBytes"`
	Removable      bool        `json:"removable"`
	ReadOnly       bool        `json:"readOnly"`
	PartitionTable *string     `json:"partitionTable"`
	System         bool        `json:"system"`
	Roles          []string    `json:"roles"`
	SharedWithData bool        `json:"sharedWithData"` // system or swap on the same disk as data (PWR-05)
	Partitions     []Partition `json:"partitions"`
}

type Partition struct {
	Name      string  `json:"name"`
	Number    int     `json:"number"`
	SizeBytes uint64  `json:"sizeBytes"`
	PartUUID  *string `json:"partUuid"`
	PartLabel *string `json:"partLabel"`
	FSType    *string `json:"fsType"`
	FSUUID    *string `json:"fsUuid"`
	Label     *string `json:"label"`
	Swap      bool    `json:"swap"`
	Mounts    []Mount `json:"mounts"`
	Usage     *Usage  `json:"usage"`
}

type Mount struct {
	Point    string `json:"point"`
	ReadOnly bool   `json:"readOnly"`
	// Bind marks a second view of an already listed filesystem (bind mount or
	// duplicate mount); its space is not counted again.
	Bind bool   `json:"bind"`
	Root string `json:"root"`
}

type Usage struct {
	TotalBytes     uint64 `json:"totalBytes"`
	UsedBytes      uint64 `json:"usedBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

// StatFS reports capacity of a mounted filesystem: total, free and available bytes.
type StatFS func(path string) (total, free, avail uint64, err error)

type Collector struct {
	HAL    hal.Reader
	StatFS StatFS
}

func (c Collector) Report() (Report, error) {
	disks, err := c.HAL.BlockDisks()
	if err != nil {
		return Report{}, err
	}
	mounts, err := c.HAL.MountInfo()
	if err != nil {
		slog.Warn("mountinfo unavailable", "err", err)
	}
	swaps, err := c.HAL.SwapPartitions()
	if err != nil {
		slog.Warn("swaps unavailable", "err", err)
	}
	isSwap := map[string]bool{}
	for _, s := range swaps {
		isSwap[s] = true
	}
	byDev := map[string][]hal.MountEntry{}
	for _, m := range mounts {
		byDev[m.DevNum] = append(byDev[m.DevNum], m)
	}

	out := Report{Disks: []Disk{}}
	for _, bd := range disks {
		out.Disks = append(out.Disks, c.disk(bd, byDev, isSwap))
	}
	sort.SliceStable(out.Disks, func(i, j int) bool {
		if out.Disks[i].System != out.Disks[j].System {
			return out.Disks[i].System
		}
		return out.Disks[i].Name < out.Disks[j].Name
	})
	return out, nil
}

func (c Collector) disk(bd hal.BlockDisk, byDev map[string][]hal.MountEntry, isSwap map[string]bool) Disk {
	u := bd.Udev
	d := Disk{
		Name: bd.Name, SizeBytes: bd.SizeBytes, Removable: bd.Removable, ReadOnly: bd.ReadOnly,
		Model: opt(bd.Model), Serial: opt(u["ID_SERIAL_SHORT"]),
		Firmware: opt(firstNonEmpty(u["ID_REVISION"], bd.Firmware)),
		Bus:      busOf(bd), PartitionTable: opt(u["ID_PART_TABLE_TYPE"]),
		Roles: []string{}, Partitions: []Partition{},
	}
	if v := strings.TrimSpace(bd.Vendor); v != "" && v != "ATA" { // libata reports the bus as vendor
		d.Vendor = &v
	}
	d.ID, d.IDStable = stableID(bd)
	if rpm, err := strconv.Atoi(u["ID_ATA_ROTATION_RATE_RPM"]); err == nil && rpm > 0 {
		d.RotationRPM = &rpm
	}
	d.Type = typeOf(bd, d.Bus, d.RotationRPM)

	roles := map[string]bool{}
	if dm := byDev[bd.DevNum]; len(dm) > 0 { // filesystem directly on the disk, no partition table
		p := c.partition(hal.BlockPartition{Name: bd.Name, DevNum: bd.DevNum, SizeBytes: bd.SizeBytes, Udev: u}, dm, isSwap[bd.Name], roles)
		d.Partitions = append(d.Partitions, p)
	}
	for _, bp := range bd.Partitions {
		d.Partitions = append(d.Partitions, c.partition(bp, byDev[bp.DevNum], isSwap[bp.Name], roles))
	}
	sort.Slice(d.Partitions, func(i, j int) bool { return d.Partitions[i].Number < d.Partitions[j].Number })
	for _, r := range []string{RoleSystem, RoleBoot, RoleSwap, RoleData} {
		if roles[r] {
			d.Roles = append(d.Roles, r)
		}
	}
	d.System = roles[RoleSystem] || roles[RoleSwap]
	d.SharedWithData = d.System && roles[RoleData]
	return d
}

func (c Collector) partition(bp hal.BlockPartition, ms []hal.MountEntry, swap bool, roles map[string]bool) Partition {
	u := bp.Udev
	p := Partition{
		Name: bp.Name, Number: bp.Number, SizeBytes: bp.SizeBytes,
		PartUUID: opt(u["ID_PART_ENTRY_UUID"]), PartLabel: opt(u["ID_PART_ENTRY_NAME"]),
		FSType: opt(u["ID_FS_TYPE"]), FSUUID: opt(u["ID_FS_UUID"]), Label: opt(u["ID_FS_LABEL"]),
		Swap: swap, Mounts: []Mount{},
	}
	if swap {
		roles[RoleSwap] = true
	}
	primary := ""
	for _, m := range ms {
		bind := m.Root != "/" || primary != ""
		if !bind {
			primary = m.Point
		}
		p.Mounts = append(p.Mounts, Mount{Point: m.Point, ReadOnly: m.ReadOnly, Bind: bind, Root: m.Root})
		if p.FSType == nil {
			p.FSType = opt(m.FSType)
		}
		if bind {
			continue
		}
		switch {
		case m.Point == "/":
			roles[RoleSystem] = true
		case m.Point == "/boot" || strings.HasPrefix(m.Point, "/boot/"):
			roles[RoleBoot] = true
		case m.Point == "/srv" || strings.HasPrefix(m.Point, "/srv/"):
			roles[RoleData] = true
		}
	}
	if primary != "" && c.StatFS != nil {
		if total, free, avail, err := c.StatFS(primary); err != nil {
			slog.Warn("statfs failed", "mount", primary, "err", err)
		} else {
			p.Usage = &Usage{TotalBytes: total, UsedBytes: total - free, AvailableBytes: avail}
		}
	}
	return p
}

// idPrefixes orders by-id names from most to least stable. WWN and EUI are
// burned into the device; bus-model-serial names come next.
var idPrefixes = []string{"wwn-", "nvme-eui.", "ata-", "nvme-", "usb-", "mmc-", "scsi-"}

func stableID(bd hal.BlockDisk) (string, bool) {
	for _, pre := range idPrefixes {
		for _, id := range bd.ByID {
			if strings.HasPrefix(id, pre) && !strings.Contains(id, "-part") {
				return id, true
			}
		}
	}
	return "dev-" + bd.Name, false
}

// FindByID returns the disk with the given stable ID.
func (r Report) FindByID(id string) (Disk, bool) {
	for _, d := range r.Disks {
		if d.ID == id {
			return d, true
		}
	}
	return Disk{}, false
}

func busOf(bd hal.BlockDisk) string {
	switch b := bd.Udev["ID_BUS"]; {
	case b == "ata" || b == "usb":
		return b
	case strings.HasPrefix(bd.Name, "nvme"):
		return "nvme"
	case strings.HasPrefix(bd.Name, "mmcblk"):
		return "mmc"
	}
	return "unknown"
}

func typeOf(bd hal.BlockDisk, bus string, rpm *int) string {
	model := strings.ToUpper(bd.Model)
	switch {
	case strings.Contains(model, "SSHD"):
		return "sshd"
	case bus == "mmc":
		return "flash"
	// USB sticks often claim to be rotational; a removable USB disk is flash.
	case bus == "usb" && bd.Removable:
		return "flash"
	case rpm != nil:
		return "hdd"
	case bd.Udev["ID_ATA_ROTATION_RATE_RPM"] == "0" || bus == "nvme" || !bd.Rotational:
		return "ssd"
	case bd.Rotational:
		return "hdd"
	}
	return "unknown"
}

func opt(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
