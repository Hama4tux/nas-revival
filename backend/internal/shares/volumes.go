package shares

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Volume is a data volume a share may live on.
type Volume struct {
	Path       string `json:"path"`
	FsType     string `json:"fsType"`
	TotalBytes uint64 `json:"totalBytes"`
	AvailBytes uint64 `json:"availBytes"`
	// ACL: the filesystem accepts POSIX ACLs (real probe).
	ACL bool `json:"acl"`
}

// Mount is one line of mountinfo.
type Mount struct{ Dev, Point, FsType, Source string }

func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s)+1 && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// ParseMountinfo parses /proc/self/mountinfo.
func ParseMountinfo(r io.Reader) []Mount {
	var out []Mount
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		sep := -1
		for i, x := range f {
			if x == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(f) < sep+3 {
			continue
		}
		out = append(out, Mount{Dev: f[2], Point: unescapeMount(f[4]), FsType: f[sep+1], Source: unescapeMount(f[sep+2])})
	}
	return out
}

// vfat only appears as a USB volume mounted by usb.mount (W4.2) with fixed
// owner and modes; ext2/ext3 sticks are mounted by the ext4 driver.
var dataFs = map[string]bool{"ext4": true, "xfs": true, "btrfs": true, "vfat": true}

// DataMounts filters the mounts that may carry shares: a mountpoint under
// /srv/, a block device source, a data filesystem and a device other than the
// one of /.
func DataMounts(ms []Mount) []Mount {
	rootDev := ""
	byPoint := map[string]Mount{}
	var order []string
	for _, m := range ms {
		if m.Point == "/" {
			rootDev = m.Dev
		}
		if _, ok := byPoint[m.Point]; !ok {
			order = append(order, m.Point)
		}
		byPoint[m.Point] = m // the last mount on a point wins
	}
	var out []Mount
	for _, p := range order {
		m := byPoint[p]
		if strings.HasPrefix(m.Point, VolumeRoot) && len(m.Point) > len(VolumeRoot) && strings.HasPrefix(m.Source, "/dev/") &&
			dataFs[m.FsType] && m.Dev != rootDev {
			out = append(out, m)
		}
	}
	return out
}

// Statfs returns total and available bytes of a path.
type Statfs func(path string) (total, avail uint64, err error)

// SysStatfs is the production Statfs.
func SysStatfs(path string) (uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize)
	return bs * uint64(st.Blocks), bs * uint64(st.Bavail), nil
}

// ACLProbe reports whether a volume accepts POSIX ACLs.
type ACLProbe func(volume string) bool

// NewACLProbe returns the real probe: it creates a temporary directory on the
// volume, sets a minimal access ACL and removes it. The result is cached per
// volume for the lifetime of the process.
func NewACLProbe(set XattrSetter) ACLProbe {
	var mu sync.Mutex
	cache := map[string]bool{}
	return func(volume string) bool {
		mu.Lock()
		defer mu.Unlock()
		if v, ok := cache[volume]; ok {
			return v
		}
		dir, err := os.MkdirTemp(volume, ".nas-revival-aclprobe-")
		if err != nil {
			return false // not cached: a read-only or full volume may recover
		}
		defer os.RemoveAll(dir)
		acl := EncodeACL([]ACLEntry{{TagUserObj, 7, idNone}, {TagGroupObj, 0, idNone}, {TagOther, 0, idNone}})
		err = set(dir, XattrAccess, acl)
		ok := err == nil
		if err != nil && !isNoSupport(err) {
			return false
		}
		cache[volume] = ok
		return ok
	}
}

var errNoVolume = errors.New("volume not mounted")

// Volumes lists the data volumes.
func Volumes(mountinfo string, statfs Statfs, probe ACLProbe) ([]Volume, error) {
	f, err := os.Open(mountinfo)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []Volume{}
	for _, m := range DataMounts(ParseMountinfo(f)) {
		v := Volume{Path: m.Point, FsType: m.FsType}
		if t, a, err := statfs(m.Point); err == nil {
			v.TotalBytes, v.AvailBytes = t, a
		}
		v.ACL = probe(m.Point)
		out = append(out, v)
	}
	return out, nil
}

// FindVolume returns the volume with this path if it is still a data mount.
func FindVolume(vols []Volume, path string) (Volume, error) {
	for _, v := range vols {
		if v.Path == path {
			return v, nil
		}
	}
	return Volume{}, errNoVolume
}
