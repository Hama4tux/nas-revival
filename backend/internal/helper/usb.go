package helper

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/storage"
)

// USB storage (W4.2, STOR-USB-01/02, ADR 0010). The helper runs in its own
// mount namespace, so it mounts through systemd (systemd-mount over D-Bus):
// the mount then exists for the whole system and systemd stops it when the
// stick disappears. Only USB partitions with a filesystem the NAS kernel
// has (ext2/3/4, FAT) are mounted, always nosuid,nodev,noexec, below
// /srv/usb, where the shared folders can use them.

// Error codes of the USB operations.
const (
	CodeUsbBusy = "usb_busy"
)

// UsbRoot is where USB partitions are mounted.
const UsbRoot = "/srv/usb"

// UsbPartition is one partition (or whole-disk filesystem) of a USB disk.
type UsbPartition struct {
	Name      string `json:"name"`
	SizeBytes uint64 `json:"sizeBytes"`
	FSType    string `json:"fsType"`
	Label     string `json:"label"`
	// MountPoint is set when mounted; Ours says it is below /srv/usb.
	MountPoint string `json:"mountPoint"`
	Ours       bool   `json:"ours"`
	ReadOnly   bool   `json:"readOnly"`
	UsedBytes  uint64 `json:"usedBytes"`
	TotalBytes uint64 `json:"totalBytes"`
	// Blocked says why the partition cannot be mounted ("" = it can).
	Blocked string `json:"blocked"`
}

// UsbDisk is one USB disk.
type UsbDisk struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Model      string         `json:"model"`
	Vendor     string         `json:"vendor"`
	SizeBytes  uint64         `json:"sizeBytes"`
	Partitions []UsbPartition `json:"partitions"`
}

// UsbList is the result of usb.list.
type UsbList struct {
	Disks []UsbDisk `json:"disks"`
}

// UsbManager lists, mounts, unmounts and ejects USB disks.
type UsbManager struct {
	Report  func() (storage.Report, error)
	Run     Runner
	DB      func() (accounts.DB, error)
	Exclude string // /etc/nsa310-copy.exclude: UUIDs of the rescue stick
	// Taken reports whether a mount point name is in use (a mount or a
	// non-empty folder); an empty folder left by an earlier mount is reused.
	Taken func(string) bool
}

var (
	usbFS        = map[string]bool{"ext2": true, "ext3": true, "ext4": true, "vfat": true}
	kernelNameRe = regexp.MustCompile(`^sd[a-z]{1,2}[0-9]{0,3}$`)
	mountNameBad = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	// The install stick of the disk swap (scripts/debian/disk-swap) is never mounted.
	protectedLabels = map[string]bool{"nrinstall": true}
)

func usbBusy(msg string) *Error { return &Error{Code: CodeUsbBusy, Message: msg} }

func (m *UsbManager) excluded() map[string]bool {
	out := map[string]bool{}
	f, err := os.Open(m.Exclude)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			out[strings.ToLower(l)] = true
		}
	}
	return out
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// List returns the USB disks with what can be done with each partition.
func (m *UsbManager) List(ctx context.Context) (UsbList, error) {
	r, err := m.Report()
	if err != nil {
		return UsbList{}, internalErr("Disky nelze přečíst.")
	}
	ex := m.excluded()
	out := UsbList{Disks: []UsbDisk{}}
	for _, d := range r.Disks {
		if d.Bus != "usb" {
			continue
		}
		ud := UsbDisk{ID: d.ID, Name: d.Name, Model: deref(d.Model), Vendor: deref(d.Vendor), SizeBytes: d.SizeBytes, Partitions: []UsbPartition{}}
		for _, p := range d.Partitions {
			up := UsbPartition{Name: p.Name, SizeBytes: p.SizeBytes, FSType: deref(p.FSType), Label: deref(p.Label)}
			for _, mt := range p.Mounts {
				if mt.Bind {
					continue
				}
				up.MountPoint, up.ReadOnly = mt.Point, mt.ReadOnly
				up.Ours = strings.HasPrefix(mt.Point, UsbRoot+"/")
				break
			}
			if p.Usage != nil {
				up.UsedBytes, up.TotalBytes = p.Usage.UsedBytes, p.Usage.TotalBytes
			}
			switch {
			case ex[strings.ToLower(deref(p.FSUUID))] || protectedLabels[up.Label]:
				up.Blocked = "Záchranná nebo instalační flashka NASu – nepřipojuje se."
			case p.Swap:
				up.Blocked = "Odkládací oddíl."
			case up.FSType == "":
				up.Blocked = "Bez souborového systému."
			case up.FSType == "exfat" || up.FSType == "ntfs":
				up.Blocked = "Jádro NASu zatím neumí " + strings.ToUpper(up.FSType) + "; připojit jde ext4 nebo FAT32."
			case !usbFS[up.FSType]:
				up.Blocked = "Nepodporovaný souborový systém " + up.FSType + "."
			case up.MountPoint != "" && !up.Ours:
				up.Blocked = "Připojeno jinde (" + up.MountPoint + ")."
			}
			ud.Partitions = append(ud.Partitions, up)
		}
		out.Disks = append(out.Disks, ud)
	}
	return out, nil
}

func (m *UsbManager) find(ctx context.Context, diskID string) (UsbDisk, error) {
	l, err := m.List(ctx)
	if err != nil {
		return UsbDisk{}, err
	}
	for _, d := range l.Disks {
		if d.ID == diskID {
			if !kernelNameRe.MatchString(d.Name) {
				return UsbDisk{}, internalErr("Neočekávaný název zařízení.")
			}
			return d, nil
		}
	}
	return UsbDisk{}, notFound("USB disk není připojený.")
}

func partOf(d UsbDisk, name string) (UsbPartition, error) {
	for _, p := range d.Partitions {
		if p.Name == name && kernelNameRe.MatchString(p.Name) {
			return p, nil
		}
	}
	return UsbPartition{}, notFound("Oddíl na disku není.")
}

// MountName makes the folder name below /srv/usb from the label (or the
// partition name) and adds -2, -3 … when it is taken.
func MountName(label, part string, taken func(string) bool) string {
	n := strings.Trim(mountNameBad.ReplaceAllString(label, "_"), "._-")
	if len(n) > 32 {
		n = n[:32]
	}
	if n == "" {
		n = "usb-" + part
	}
	name := n
	for i := 2; taken(name) && i < 100; i++ {
		name = n + "-" + strconv.Itoa(i)
	}
	return name
}

// Mount mounts a partition below /srv/usb.
func (m *UsbManager) Mount(ctx context.Context, diskID, part string, readOnly bool, actor string) (UsbList, error) {
	d, err := m.find(ctx, diskID)
	if err != nil {
		return UsbList{}, err
	}
	p, err := partOf(d, part)
	if err != nil {
		return UsbList{}, err
	}
	if p.MountPoint != "" && p.Ours {
		return UsbList{}, usbBusy("Oddíl už je připojený v " + p.MountPoint + ".")
	}
	if p.Blocked != "" {
		return UsbList{}, invalid("partition", p.Blocked)
	}
	opts := []string{"nosuid", "nodev", "noexec", "noatime"}
	if readOnly {
		opts = append(opts, "ro")
	}
	if p.FSType == "vfat" {
		db, err := m.DB()
		if err != nil {
			return UsbList{}, internalErr("Nelze načíst uživatelské účty.")
		}
		g, ok := db.Group(accounts.UsersGroup)
		if !ok {
			return UsbList{}, internalErr("Skupina nas-users neexistuje.")
		}
		// FAT has no owners: everything is root:nas-users 0770/0660, the
		// shared folders and Samba decide who sees what.
		opts = append(opts, "uid=0", "gid="+strconv.Itoa(g.GID), "fmask=0117", "dmask=0007", "utf8", "shortname=mixed", "flush")
	}
	point := path.Join(UsbRoot, MountName(p.Label, p.Name, func(n string) bool { return m.Taken(path.Join(UsbRoot, n)) }))
	fs := p.FSType
	if fs == "ext2" || fs == "ext3" {
		fs = "ext4"
	}
	if out, err := m.Run(ctx, "systemd-mount", "--no-ask-password", "--collect", "--fsck=no", "-t", fs, "-o", strings.Join(opts, ","), "/dev/"+p.Name, point); err != nil {
		slog.Warn("helper: usb.mount failed", "dev", p.Name, "err", err, "out", strings.TrimSpace(string(out)))
		return UsbList{}, internalErr("Oddíl se nepodařilo připojit (poškozený souborový systém? zkuste ho opravit v počítači).")
	}
	slog.Info("helper: usb.mount", "disk", diskID, "dev", p.Name, "point", point, "fs", p.FSType, "readOnly", readOnly, "user", actor)
	return m.List(ctx)
}

func (m *UsbManager) unmount(ctx context.Context, p UsbPartition) error {
	if out, err := m.Run(ctx, "systemd-umount", "--no-ask-password", p.MountPoint); err != nil {
		slog.Warn("helper: usb unmount failed", "point", p.MountPoint, "err", err, "out", strings.TrimSpace(string(out)))
		return usbBusy("Oddíl " + p.MountPoint + " nejde odpojit – nejspíš se s ním právě pracuje (otevřené soubory přes SMB, Soubory, kopírování). Zkuste to za chvíli.")
	}
	return nil
}

// Unmount unmounts a partition the helper mounted.
func (m *UsbManager) Unmount(ctx context.Context, diskID, part, actor string) (UsbList, error) {
	d, err := m.find(ctx, diskID)
	if err != nil {
		return UsbList{}, err
	}
	p, err := partOf(d, part)
	if err != nil {
		return UsbList{}, err
	}
	if p.MountPoint == "" {
		return m.List(ctx)
	}
	if !p.Ours {
		return UsbList{}, invalid("partition", "Oddíl připojil někdo jiný ("+p.MountPoint+"); odsud ho odpojit nejde.")
	}
	if err := m.unmount(ctx, p); err != nil {
		return UsbList{}, err
	}
	slog.Info("helper: usb.unmount", "disk", diskID, "dev", p.Name, "point", p.MountPoint, "user", actor)
	return m.List(ctx)
}

// Eject unmounts every partition of the disk, flushes it and removes the
// device (nas-revival-usb-eject@<name>.service), so it can be pulled out.
func (m *UsbManager) Eject(ctx context.Context, diskID, actor string) (UsbList, error) {
	d, err := m.find(ctx, diskID)
	if err != nil {
		return UsbList{}, err
	}
	for _, p := range d.Partitions {
		if p.MountPoint != "" && !p.Ours {
			return UsbList{}, usbBusy("Oddíl " + p.Name + " používá jiná úloha (" + p.MountPoint + "); disk teď nejde vysunout.")
		}
	}
	for _, p := range d.Partitions {
		if p.MountPoint == "" {
			continue
		}
		if err := m.unmount(ctx, p); err != nil {
			return UsbList{}, err
		}
	}
	if out, err := m.Run(ctx, "systemctl", "start", "nas-revival-usb-eject@"+d.Name+".service"); err != nil {
		slog.Warn("helper: usb eject failed", "dev", d.Name, "err", err, "out", strings.TrimSpace(string(out)))
		return UsbList{}, internalErr("Disk je odpojený, ale nepodařilo se ho vypnout; vytáhnout ho jde, data jsou zapsaná.")
	}
	slog.Info("helper: usb.eject", "disk", diskID, "dev", d.Name, "user", actor)
	return m.List(ctx)
}

// handleUsb runs the USB operations.
func (d *Dispatcher) handleUsb(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		DiskID    string `json:"diskId"`
		Partition string `json:"partition"`
		ReadOnly  bool   `json:"readOnly"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	switch req.Op {
	case "usb.list":
		return d.Usb.List(ctx)
	case "usb.mount":
		return d.Usb.Mount(ctx, a.DiskID, a.Partition, a.ReadOnly, user)
	case "usb.unmount":
		return d.Usb.Unmount(ctx, a.DiskID, a.Partition, user)
	case "usb.eject":
		return d.Usb.Eject(ctx, a.DiskID, user)
	}
	return nil, invalid("", "Neznámá operace.")
}

// SysTaken is the production Taken.
func SysTaken(p string) bool {
	st, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil || !st.IsDir() {
		return true
	}
	pst, err := os.Stat(path.Dir(p))
	if err != nil {
		return true
	}
	if a, ok := st.Sys().(*syscall.Stat_t); ok {
		if b, ok := pst.Sys().(*syscall.Stat_t); ok && a.Dev != b.Dev {
			return true // a mount point
		}
	}
	f, err := os.Open(p)
	if err != nil {
		return true
	}
	defer f.Close()
	names, _ := f.Readdirnames(1)
	return len(names) > 0
}
