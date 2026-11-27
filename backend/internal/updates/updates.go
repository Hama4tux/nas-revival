// Package updates is the Debian part of the Update Manager (UPD-01, ADR 0016):
// it parses apt-get output and runs the check and the upgrade as the root
// job nas-revival-updates@.service, outside the helper's cgroup, so a restart
// of the helper or the web service never kills a running dpkg.
package updates

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"nas-revival/backend/internal/atomicfile"
)

// Jobs of nas-revival-updates@.service.
const (
	JobCheck   = "check"
	JobUpgrade = "upgrade"
)

// DefaultStateDir is the StateDirectory of nas-revival-updates@.service.
const DefaultStateDir = "/var/lib/nas-revival-updates"

// ServiceName is the unit of one job.
func ServiceName(job string) string { return "nas-revival-updates@" + job + ".service" }

// ValidJob reports whether job is a known instance.
func ValidJob(job string) bool { return job == JobCheck || job == JobUpgrade }

// Package is one pending upgrade or new dependency.
type Package struct {
	Name string `json:"name"`
	// From is the installed version; empty for a newly installed dependency.
	From   string `json:"from,omitempty"`
	To     string `json:"to"`
	Origin string `json:"origin"` // e.g. "Debian-Security:13/stable-security"
	// Security: the version comes from the Debian security archive.
	Security bool `json:"security"`
	// Size of the .deb to download; 0 when already cached or unknown.
	Size int64 `json:"size"`
}

// Upgrade is the current or last run of the upgrade job.
type Upgrade struct {
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	// Result: running, ok, failed.
	Result  string `json:"result"`
	Message string `json:"message,omitempty"`
	// Total packages to install and how many are set up so far.
	Total int `json:"total"`
	Done  int `json:"done"`
	// Packages installed (name → version) once finished.
	Packages []string `json:"packages,omitempty"`
}

// Upgrade results.
const (
	ResultRunning = "running"
	ResultOK      = "ok"
	ResultFailed  = "failed"
)

// State is state.json, written only by the job and read by the helper.
type State struct {
	// CheckedAt is the time of the last check that refreshed the package lists.
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	// CheckError is the reason the last check failed (empty when it worked).
	CheckError string `json:"checkError,omitempty"`
	// ListedAt is when Packages was computed; the upgrade request must name
	// it, so the user installs the list they saw.
	ListedAt      *time.Time `json:"listedAt,omitempty"`
	Packages      []Package  `json:"packages"`
	KeptBack      []string   `json:"keptBack"`
	DownloadBytes int64      `json:"downloadBytes"`
	Upgrade       *Upgrade   `json:"upgrade,omitempty"`
	// RestartPackages were upgraded since the last boot and are used by
	// long-running processes; a restart of the NAS loads the new versions.
	RestartPackages []string   `json:"restartPackages"`
	RestartSince    *time.Time `json:"restartSince,omitempty"` // boot time those packages refer to
}

// Security counts the security updates.
func (s State) Security() int {
	n := 0
	for _, p := range s.Packages {
		if p.Security {
			n++
		}
	}
	return n
}

// StatePath is state.json in dir.
func StatePath(dir string) string { return filepath.Join(dir, "state.json") }

// Load reads state.json; a missing file is the empty state.
func Load(dir string) (State, error) {
	raw, err := os.ReadFile(StatePath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return State{Packages: []Package{}, KeptBack: []string{}, RestartPackages: []string{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, err
	}
	if s.Packages == nil {
		s.Packages = []Package{}
	}
	if s.KeptBack == nil {
		s.KeptBack = []string{}
	}
	if s.RestartPackages == nil {
		s.RestartPackages = []string{}
	}
	return s, nil
}

// Save writes state.json atomically (0644: it holds only package names).
func Save(dir string, s State) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(StatePath(dir), append(raw, '\n'), 0o644)
}

// "Inst name [from] (to origin [arch])" – the [from] part is missing for a
// new package. Several origins are separated by ", " inside the parentheses.
var instRE = regexp.MustCompile(`^Inst (\S+) (?:\[([^\]]+)\] )?\((\S+) ([^\[]*?)\s*\[[^\]]+\]\)`)

// ParseSimulation reads `apt-get -s upgrade --with-new-pkgs` (LANG=C).
func ParseSimulation(out []byte) (pkgs []Package, kept []string) {
	pkgs, kept = []Package{}, []string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	inKept := false
	for sc.Scan() {
		line := sc.Text()
		if inKept {
			if strings.HasPrefix(line, "  ") {
				kept = append(kept, strings.Fields(line)...)
				continue
			}
			inKept = false
		}
		if strings.HasPrefix(line, "The following packages have been kept back:") {
			inKept = true
			continue
		}
		m := instRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		origin := strings.TrimSpace(m[4])
		pkgs = append(pkgs, Package{Name: m[1], From: m[2], To: m[3], Origin: origin,
			Security: strings.Contains(strings.ToLower(origin), "-security")})
	}
	sort.Strings(kept)
	return pkgs, kept
}

// ParsePrintURIs reads `apt-get --print-uris -qq upgrade --with-new-pkgs`:
// "'uri' name_version_arch.deb size hash" → size per package name.
func ParsePrintURIs(out []byte) map[string]int64 {
	sizes := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || !strings.HasPrefix(f[0], "'") {
			continue
		}
		file, err := url.PathUnescape(f[1])
		if err != nil {
			continue
		}
		name, _, ok := strings.Cut(file, "_")
		n, err := strconv.ParseInt(f[2], 10, 64)
		if !ok || err != nil || n < 0 {
			continue
		}
		sizes[name] = n
	}
	return sizes
}

var setupRE = regexp.MustCompile(`^Setting up ([^ :]+)(?::[^ ]+)? \(([^)]+)\)`)

// SetupLine returns the package and version of a dpkg "Setting up name:arch (version) ..." line.
func SetupLine(line string) (name, version string, ok bool) {
	m := setupRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// restartLibs are used by every long-running process (systemd, sshd, smbd,
// the web service): until a restart the old, possibly vulnerable copy stays
// in memory. Debian without needrestart has no other signal.
var restartLibs = map[string]bool{
	"libc6": true, "libc-bin": true, "systemd": true, "libsystemd0": true, "libsystemd-shared": true,
	"udev": true, "libudev1": true, "dbus": true, "libdbus-1-3": true, "libssl3t64": true, "openssl": true,
	"libgnutls30t64": true, "libpam0g": true, "libpam-modules": true, "libselinux1": true, "zlib1g": true,
	"libcrypt1": true, "libgcrypt20": true, "libzstd1": true, "liblzma5": true, "libcap2": true,
}

// NeedsRestart lists the installed packages that call for a restart.
func NeedsRestart(installed []string) []string {
	out := []string{}
	for _, p := range installed {
		name, _, _ := strings.Cut(p, " ")
		if restartLibs[name] {
			out = append(out, name)
		}
	}
	return out
}

// LastLines returns up to n non-empty trimmed lines of out, for an error message.
func LastLines(out []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if len(l) > 300 {
			l = l[:300]
		}
		keep = append([]string{strings.ToValidUTF8(l, "")}, keep...)
	}
	return strings.Join(keep, "\n")
}

// BootTime reads the boot time (btime) from <procRoot>/stat.
func BootTime(procRoot string) (time.Time, error) {
	raw, err := os.ReadFile(filepath.Join(procRoot, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(l, "btime "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(n, 0).UTC(), nil
		}
	}
	return time.Time{}, errors.New("btime missing in " + filepath.Join(procRoot, "stat"))
}
