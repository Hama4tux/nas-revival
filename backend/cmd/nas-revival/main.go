// Command nas-revival serves the NAS administration UI and API over HTTPS.
//
//	nas-revival [-listen :8443] [-config /etc/nas-revival] [-state /var/lib/nas-revival]
//	nas-revival set-password [-config /etc/nas-revival] [-user admin]
//	nas-revival helper [-socket /run/nas-revival-helper/helper.sock] [-user nas-revival]
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/api"
	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/diskactivity"
	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/hal"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/smb"
	"nas-revival/backend/internal/storage"
	"nas-revival/backend/internal/system"
	"nas-revival/backend/internal/tlscert"
	"nas-revival/backend/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	var err error
	if len(os.Args) > 1 && os.Args[1] == "set-password" {
		err = setPassword(os.Args[2:])
	} else if len(os.Args) > 1 && os.Args[1] == "helper" {
		err = runHelper(os.Args[2:])
	} else {
		err = serve(os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "nas-revival:", err)
		os.Exit(1)
	}
}

func serve(args []string) error {
	fl := flag.NewFlagSet("nas-revival", flag.ContinueOnError)
	listen := fl.String("listen", ":8443", "HTTPS listen address")
	configDir := fl.String("config", "/etc/nas-revival", "configuration directory (admin.json)")
	stateDir := fl.String("state", "/var/lib/nas-revival", "state directory (generated TLS certificate)")
	helperSock := fl.String("helper-socket", "/run/nas-revival-helper/helper.sock", "privileged helper socket")
	root := fl.String("root", "/", "file-system root for procfs/sysfs (testing only)")
	if err := fl.Parse(args); err != nil {
		return err
	}

	// admin.json is the recovery administrator; NAS accounts log in with
	// their web hash from credentials.json (ADR 0004, W2.2).
	dir := &auth.Directory{Paths: accounts.SystemPaths,
		Web: accounts.WebPasswords{Path: filepath.Join(*configDir, "credentials.json"), OwnerUID: -1, OwnerGID: -1}}
	creds, err := auth.LoadCredentials(filepath.Join(*configDir, "admin.json"))
	switch {
	case err == nil:
		dir.Admin = &creds
	case errors.Is(err, os.ErrNotExist):
		slog.Info("no recovery administrator (admin.json); only NAS accounts can log in")
	default:
		return fmt.Errorf("load administrator: %w", err)
	}
	verifier, err := auth.NewLookupVerifier(dir.Hash)
	if err != nil {
		return err
	}
	certs, err := tlscert.New(filepath.Join(*stateDir, "tls"))
	if err != nil {
		return fmt.Errorf("tls certificate: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reader := hal.Reader{Root: *root}
	smartSvc := &smart.Service{
		Run:     smart.ExecRunner,
		Devices: func() []string { return smartDevices(reader) },
		TTL:     10 * time.Minute,
		Now:     time.Now,
	}
	diskPower := &diskpower.Reader{Run: diskpower.ExecRunner, Now: time.Now, ConfigPath: diskpower.DefaultConfigPath}
	collector := system.NewCollector(*root, system.NewServiceChecker(system.DefaultUnits))
	collector.Smart = smartSvc
	collector.DiskPower = func(ctx context.Context, dev string) string { return diskPower.Read(ctx, dev).State }
	// One CHECK POWER MODE per minute at most: cheap, and keeps an open
	// dashboard from poking the drive every few seconds.
	collector.PowerTTL = time.Minute
	snapshots := system.NewSnapshots(collector.Collect)
	go snapshots.Run(ctx)

	srv := &api.Server{
		Verifier: verifier,
		Roles:    dir,
		Sessions: auth.NewSessions(),
		Limiter:  auth.NewLimiter(),
		Overview: snapshots,
		Smart:    smartSvc,
		UI:       web.Handler(web.Dist()),
		TLS:      certs,
	}
	hc := &helper.Client{Socket: *helperSock}
	srv.System = systemSource{root: *root, profile: system.DetectProfile(*root)}
	srv.Network, srv.Time = hc, hc
	srv.Storage = storage.Collector{HAL: reader, StatFS: statFS}
	// Short TTL: the UI polls progress of a running self-test.
	srv.SmartDetail = &smart.DetailService{Run: smart.DetailExecRunner, TTL: 5 * time.Second, Now: time.Now}
	srv.SmartTests = hc
	srv.DiskPower = diskPower
	srv.DiskPowerSet = hc
	srv.DiskActivity = hc
	srv.SystemPower = hc
	srv.Config = hc
	srv.Users = hc
	srv.Shares = hc
	srv.Services = hc
	srv.Smb = hc
	srv.Nfs = hc

	hs := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: certs.GetCertificate},
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", *listen)
		errc <- hs.ListenAndServeTLS("", "")
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// smartDevices returns fixed disks only; USB sticks usually have no SMART.
func smartDevices(r hal.Reader) []string {
	ds, err := r.Disks()
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range ds {
		if !d.Removable && strings.HasPrefix(d.Name, "sd") {
			out = append(out, d.Name)
		}
	}
	return out
}

func setPassword(args []string) error {
	fl := flag.NewFlagSet("set-password", flag.ContinueOnError)
	configDir := fl.String("config", "/etc/nas-revival", "configuration directory")
	user := fl.String("user", "admin", "administrator user name")
	if err := fl.Parse(args); err != nil {
		return err
	}
	pw, err := readPassword()
	if err != nil {
		return err
	}
	if pw == "" {
		return errors.New("password must not be empty")
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	path := filepath.Join(*configDir, "admin.json")
	if err := auth.SaveCredentials(path, auth.Credentials{Username: *user, Hash: hash}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "password saved to %s; restart the service to apply\n", path)
	return nil
}

// readPassword asks twice on a terminal, or reads one line from a pipe.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no password on stdin")
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "New password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Repeat password: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("passwords do not match")
	}
	return string(a), nil
}

// runHelper is the root-side privileged helper (ADR 0003).
func runHelper(args []string) error {
	fl := flag.NewFlagSet("helper", flag.ContinueOnError)
	sock := fl.String("socket", "/run/nas-revival-helper/helper.sock", "socket path")
	userName := fl.String("user", "nas-revival", "the only user allowed to connect")
	iface := fl.String("iface", "enp1s0", "managed network interface")
	state := fl.String("state", "/var/lib/nas-revival-helper", "state directory (pending change, backup)")
	netFile := fl.String("network-file", "/etc/systemd/network/20-wired.network", "networkd file to manage")
	dropin := fl.String("timesyncd-dropin", "/etc/systemd/timesyncd.conf.d/50-nas-revival.conf", "timesyncd drop-in to manage")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if !helper.ValidIfaceName(*iface) {
		return errors.New("invalid interface name")
	}
	uid, gid, err := helper.LookupUser(*userName)
	if err != nil {
		return err
	}
	nm := helper.NewManager(helper.NetworkPaths{NetworkFile: *netFile, StateDir: *state, SysClassNet: "/sys/class/net"}, *iface, helper.ExecRunner)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	nm.Recover(ctx)
	dp := &helper.DiskPowerManager{Resolve: resolvePowerDisk, Disks: powerDisks, Run: helper.ExecRunner,
		ConfigPath: diskpower.DefaultConfigPath}
	sp := &helper.SystemPowerManager{Disks: powerDisks, Run: helper.ExecRunner, ProcRoot: "/proc",
		NetPending: nm.HasPending, Sync: syscall.Sync, Delay: 2 * time.Second,
		After: func(d time.Duration, f func()) { time.AfterFunc(d, f) }}
	tm := &helper.TimeManager{Dropin: *dropin, Run: helper.ExecRunner}
	// Config history (CFG-02..04): the registry of managed files. The TLS key
	// is owned by the web process and is deliberately not part of it.
	store := &config.Store{Dir: filepath.Join(*state, "config"),
		Files: []config.File{
			{ID: helper.FileNetwork, Path: *netFile},
			{ID: helper.FileTime, Path: *dropin},
			{ID: helper.FileDiskPower, Path: diskpower.DefaultConfigPath},
			{ID: helper.FileShares, Path: shares.DefaultConfigPath},
			{ID: helper.FileSmb, Path: smb.DefaultConfigPath},
			{ID: helper.FileNfs, Path: nfs.DefaultConfigPath},
		},
		Pending: func(id string) bool { return id == helper.FileNetwork && nm.HasPending() }}
	acls := shares.NewACLProbe(shares.SysSetxattr)
	shm := &helper.ShareManager{Path: shares.DefaultConfigPath,
		Accounts:  accounts.Paths{Passwd: accounts.SystemPaths.Passwd, Group: accounts.SystemPaths.Group},
		Mountinfo: "/proc/self/mountinfo", Statfs: shares.SysStatfs, Probe: acls,
		SetXattr: shares.SysSetxattr, Chown: shares.SysLchown, Run: helper.ExecRunner, LookPath: exec.LookPath,
		SambaDir: "/etc/samba", SambaConf: shares.SambaConfPath, SambaMain: "/etc/samba/smb.conf", Now: time.Now}
	nfsm := &helper.NfsManager{Path: nfs.DefaultConfigPath, Exports: nfs.ExportsPath, Shares: shares.DefaultConfigPath,
		Accounts: shm.Accounts, ClientsDir: nfs.ClientsDir, Filesystems: "/proc/filesystems",
		Run: helper.ExecRunner, LookPath: exec.LookPath}
	shm.NfsExported, shm.NfsRefresh = nfsm.Exported, nfsm.Refresh
	smbm := &helper.SmbManager{Path: smb.DefaultConfigPath, Conf: smb.ConfPath, Fragment: shares.SambaConfPath,
		Run: helper.ExecRunner, LookPath: exec.LookPath, SharePaths: sharePaths}
	var cm *helper.ConfigManager
	if err := store.Init(); err != nil {
		slog.Error("helper: config history disabled", "err", err)
	} else {
		cm = &helper.ConfigManager{Store: store, Net: nm, Time: tm, Power: dp, Shares: shm, Smb: smbm, Nfs: nfsm}
	}
	// Boot task disk-power: hdparm settings do not survive a power cycle.
	go dp.ApplyAll(ctx)
	// Recycle bins of the shared folders (SHARE-04): at start and daily.
	go shm.RunRecycleLoop(ctx)
	ln, err := helper.Listen(*sock, gid)
	if err != nil {
		return err
	}
	slog.Info("helper listening", "socket", *sock, "uid", uid)
	return helper.Serve(ctx, ln, uid, &helper.Dispatcher{
		Net:      nm,
		Time:     tm,
		Smart:    &helper.SmartManager{Resolve: resolveSmartDisk, Run: helper.ExecRunner},
		Power:    dp,
		SysPower: sp,
		Config:   cm,
		Shares:   shm,
		Smb:      smbm,
		Nfs:      nfsm,
		Activity: &helper.ActivityManager{Measure: diskactivity.Reader{Root: "/"}},
		Services: &helper.ServiceManager{Run: helper.ExecRunner, ProcRoot: "/proc", CgroupRoot: "/sys/fs/cgroup",
			Now: time.Now, NumCPU: runtime.NumCPU(), PageSize: os.Getpagesize()},
		Users: &helper.UserManager{Paths: accounts.SystemPaths,
			// The web process (uid of -user) reads the web hashes at login.
			Web: accounts.WebPasswords{Path: accounts.DefaultWebPasswords, OwnerUID: int(uid), OwnerGID: gid},
			Run: helper.ExecRunner, RunStdin: helper.ExecStdinRunner, Hash: auth.HashPassword, Now: time.Now,
			Samba: func() bool { _, err := exec.LookPath("smbpasswd"); return err == nil }},
	})
}

// sharePaths maps share folders to share names for the SMB open file list.
func sharePaths() map[string]string {
	c, err := shares.Load(shares.DefaultConfigPath)
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(c.Shares))
	for _, s := range c.Shares {
		m[s.Path()] = s.Name
	}
	return m
}

// resolveSmartDisk maps a stable disk ID to its kernel name from sysfs/udev.
func resolveSmartDisk(id string) (helper.SmartDisk, bool, error) {
	d, ok, err := storage.ResolveDisk(hal.Reader{Root: "/"}, id)
	return helper.SmartDisk{Name: d.Name, Capable: d.SMARTCapable()}, ok, err
}

// powerDisks lists disks with the system-disk flag the default profile needs.
func powerDisks() ([]helper.PowerDisk, error) {
	rep, err := storage.Collector{HAL: hal.Reader{Root: "/"}, StatFS: statFS}.Report()
	if err != nil {
		return nil, err
	}
	out := make([]helper.PowerDisk, 0, len(rep.Disks))
	for _, d := range rep.Disks {
		out = append(out, helper.PowerDisk{ID: d.ID, Name: d.Name, Capable: d.SMART && d.IDStable, System: d.System})
	}
	return out, nil
}

func resolvePowerDisk(id string) (helper.PowerDisk, bool, error) {
	ds, err := powerDisks()
	if err != nil {
		return helper.PowerDisk{}, false, err
	}
	for _, d := range ds {
		if d.ID == id {
			return d, true, nil
		}
	}
	return helper.PowerDisk{}, false, nil
}

func statFS(path string) (total, free, avail uint64, err error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0, 0, 0, err
	}
	bs := uint64(s.Bsize)
	return uint64(s.Blocks) * bs, uint64(s.Bfree) * bs, uint64(s.Bavail) * bs, nil
}

// version is set at build time (-ldflags "-X main.version=…", scripts/webui/build.sh).
var version = "dev"

// systemSource reads /system/info live; the profile is detected once at start.
type systemSource struct {
	root    string
	profile system.Profile
}

func (s systemSource) Info() (system.Info, error)        { return system.SystemInfo(s.root, version) }
func (s systemSource) Capabilities() system.Capabilities { return system.CapabilitiesOf(s.profile) }
