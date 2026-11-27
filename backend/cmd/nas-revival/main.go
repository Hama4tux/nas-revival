// Command nas-revival serves the NAS administration UI and API over HTTPS.
//
//	nas-revival [-listen :8443] [-config /etc/nas-revival] [-state /var/lib/nas-revival]
//	nas-revival set-password [-config /etc/nas-revival] [-user admin]
//	nas-revival task run [-config /etc/nas-revival] [-state /var/lib/nas-revival] <id>
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
	"nas-revival/backend/internal/hardware"
	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/monitor"
	"nas-revival/backend/internal/netinfo"
	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/notify"
	"nas-revival/backend/internal/procs"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/smart"
	"nas-revival/backend/internal/smb"
	"nas-revival/backend/internal/sshd"
	"nas-revival/backend/internal/storage"
	"nas-revival/backend/internal/system"
	"nas-revival/backend/internal/tasks"
	"nas-revival/backend/internal/tlscert"
	"nas-revival/backend/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	var err error
	if len(os.Args) > 1 && os.Args[1] == "set-password" {
		err = setPassword(os.Args[2:])
	} else if len(os.Args) > 2 && os.Args[1] == "task" && os.Args[2] == "import-pull" {
		err = importPull(os.Args[3:])
	} else if len(os.Args) > 2 && os.Args[1] == "task" && os.Args[2] == "migrate-backup-dest" {
		err = migrateBackupDest(os.Args[3:])
	} else if len(os.Args) > 2 && os.Args[1] == "task" && os.Args[2] == "run" {
		err = runTask(os.Args[3:])
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
	collector.Backups = (&tasks.BackupStatusSource{ConfigPath: tasks.DefaultConfigPath, HistoryPath: tasks.DefaultBackupHistoryPath, TTL: time.Minute, Now: time.Now}).Statuses
	collector.DiskPower = func(ctx context.Context, dev string) string { return diskPower.Read(ctx, dev).State }
	// One CHECK POWER MODE per minute at most: cheap, and keeps an open
	// dashboard from poking the drive every few seconds.
	collector.PowerTTL = time.Minute
	hwSettings := &hardware.Cached{Path: hardware.DefaultConfigPath, TTL: 10 * time.Second, Now: time.Now}
	collector.Thresholds = func() hardware.Thresholds { return hwSettings.Get().Thresholds }
	// PWR-04: standby history and wear counters, warnings into health.
	dhs := &diskHistorySampler{storage: storage.Collector{HAL: reader, StatFS: statFS}, power: diskPower, smart: smartSvc}
	dhs.tracker = diskpower.NewTracker(filepath.Join(*stateDir, "disk-power-history.json"), dhs.sample)
	collector.DiskWear = dhs.warnings
	hc := &helper.Client{Socket: *helperSock}
	// HW-01/HW-04: journal events on threshold changes, red LED blinks while critical.
	watcher := &hardware.Watcher{SetFault: hc.LEDFault, Settings: hwSettings.Get}
	// W4.5: health problems become lasting notifications.
	notices := notify.Open(filepath.Join(*stateDir, "notifications.json"), time.Now)
	snapshots := system.NewSnapshots(func(ctx context.Context) (system.Overview, error) {
		o, err := collector.Collect(ctx)
		if err == nil {
			watcher.Update(ctx, o.Health.Status == system.HealthCrit, temperatureProblems(o.Health))
			notices.Update(notifyProblems(o.Health))
		}
		return o, err
	})
	go snapshots.Run(ctx)
	resources := monitor.New(*root)
	go resources.Run(ctx)
	hw := &hardwareSource{root: *root, hal: reader, profile: system.DetectProfile(*root), smart: smartSvc, settings: hwSettings}
	hw.history = hardware.NewHistory(hw.reading)
	go hw.history.Run(ctx)
	trackerDone := make(chan struct{})
	go func() { dhs.tracker.Run(ctx); close(trackerDone) }()

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
	srv.System = systemSource{root: *root, profile: system.DetectProfile(*root)}
	srv.Network, srv.Time = hc, hc
	srv.Storage = storage.Collector{HAL: reader, StatFS: statFS}
	// Short TTL: the UI polls progress of a running self-test.
	srv.SmartDetail = &smart.DetailService{Run: smart.DetailExecRunner, TTL: 5 * time.Second, Now: time.Now}
	srv.SmartTests = hc
	srv.DiskPower = diskPower
	srv.DiskPowerSet = hc
	srv.DiskPowerHistory = dhs.tracker
	srv.DiskActivity = hc
	srv.SystemPower = hc
	srv.Config = hc
	srv.Users = hc
	srv.Shares = hc
	srv.Services = hc
	srv.Smb = hc
	srv.Nfs = hc
	srv.Ssh = hc
	srv.Tasks = hc
	srv.Backup = hc
	srv.Files = hc
	srv.Notifications = notices
	srv.Usb = hc
	srv.NetInfo = netinfo.Reader{Root: "/", Run: netinfo.Runner(helper.ExecRunner), Now: time.Now}
	srv.NetAdmin = hc
	srv.Monitor = resources
	srv.Processes = &procs.Reader{Root: "/"}
	srv.ProcessKill = hc
	srv.Hardware, srv.HardwareAdmin = hw, hc
	srv.TaskStatus = api.TaskStatus{Run: helper.ExecRunner, HistoryPath: filepath.Join(*stateDir, "task-history.json"),
		BackupHistoryPath: tasks.DefaultBackupHistoryPath}

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
	err = hs.Shutdown(shutdownCtx)
	<-trackerDone // the history saves what is pending on exit
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	sp := &helper.SystemPowerManager{Disks: powerDisks, Run: helper.ExecRunner, ProcRoot: "/proc", HeavyLock: helper.DefaultHeavyLock,
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
			{ID: helper.FileSsh, Path: sshd.DefaultConfigPath},
		},
		Pending: func(id string) bool { return id == helper.FileNetwork && nm.HasPending() }}
	shm := newShareManager()
	nfsm := &helper.NfsManager{Path: nfs.DefaultConfigPath, Exports: nfs.ExportsPath, Shares: shares.DefaultConfigPath,
		Accounts: shm.Accounts, ClientsDir: nfs.ClientsDir, Filesystems: "/proc/filesystems",
		Run: helper.ExecRunner, LookPath: exec.LookPath}
	shm.NfsExported, shm.NfsRefresh = nfsm.Exported, nfsm.Refresh
	smbm := &helper.SmbManager{Path: smb.DefaultConfigPath, Conf: smb.ConfPath, Fragment: shares.SambaConfPath,
		Run: helper.ExecRunner, LookPath: exec.LookPath, SharePaths: sharePaths}
	sshm := &helper.SshManager{Path: sshd.DefaultConfigPath, DropIn: sshd.DropInPath, KeysDir: sshd.KeysDir,
		Chroot: sshd.ChrootDir, Accounts: shm.Accounts, Run: helper.ExecRunner}
	var cm *helper.ConfigManager
	if err := store.Init(); err != nil {
		slog.Error("helper: config history disabled", "err", err)
	} else {
		cm = &helper.ConfigManager{Store: store, Net: nm, Time: tm, Power: dp, Shares: shm, Smb: smbm, Nfs: nfsm, Ssh: sshm}
	}
	// Boot task disk-power: hdparm settings do not survive a power cycle.
	go dp.ApplyAll(ctx)
	// Scheduled tasks (W3.3): timers follow tasks.json; the default "recycle" task
	// replaces the old in-process recycle bin loop.
	tkm := &helper.TaskManager{Path: tasks.DefaultConfigPath, UnitDir: "/etc/systemd/system", Run: helper.ExecRunner,
		SharesPath: shares.DefaultConfigPath}
	if err := tkm.Reconcile(ctx); err != nil {
		slog.Error("helper: task timers", "err", err)
	}
	// Backup pull jobs (W4.3): key, host keys; the destination is a backupTarget share.
	bkm := helper.NewBackupManager(*iface, helper.ExecRunner)
	shm.BackupVolume = tasks.BackupVolume
	shm.InUseAsBackupTarget = tasks.BackupTargetUser(tasks.DefaultConfigPath)
	// The recovery administrator sees every share in the File Station; admin.json
	// is re-read on every call because set-password may change it.
	recovery := func() string {
		c, err := auth.LoadCredentials("/etc/nas-revival/admin.json")
		if err != nil {
			return ""
		}
		return c.Username
	}
	files := &helper.FileManager{Config: shm.LoadConfig, DB: shm.LoadDB, Recovery: recovery, HasACL: shm.Probe,
		Chown: helper.SysFileChown, Statfs: helper.SysFstatfs, Now: time.Now}
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
		Ssh:      sshm,
		Tasks:    tkm,
		Backup:   bkm,
		Usb: &helper.UsbManager{Report: storage.Collector{HAL: hal.Reader{Root: "/"}, StatFS: statFS}.Report,
			Run: helper.ExecRunner, DB: shm.LoadDB, Exclude: "/etc/nsa310-copy.exclude", Taken: helper.SysTaken},
		Files:    files,
		Restore:  &helper.RestoreManager{Backup: bkm, Files: files, Lower: helper.SysLowerPriority},
		Hostname: &helper.HostnameManager{HostsFile: "/etc/hosts", Run: helper.ExecRunner},
		Diag:     helper.NewDiagManager(*iface, helper.ExecRunner),
		Procs:    &helper.ProcessManager{Root: "/"},
		Hardware: helper.NewHardwareManager(helper.ExecRunner),
		Activity: &helper.ActivityManager{Measure: diskactivity.Reader{Root: "/"}},
		Services: &helper.ServiceManager{Run: helper.ExecRunner, ProcRoot: "/proc", CgroupRoot: "/sys/fs/cgroup",
			Now: time.Now, NumCPU: runtime.NumCPU(), PageSize: os.Getpagesize()},
		Users: &helper.UserManager{Paths: accounts.SystemPaths,
			// The web process (uid of -user) reads the web hashes at login.
			Web: accounts.WebPasswords{Path: accounts.DefaultWebPasswords, OwnerUID: int(uid), OwnerGID: gid},
			Run: helper.ExecRunner, RunStdin: helper.ExecStdinRunner, Hash: auth.HashPassword, Now: time.Now,
			SshKeysDir: sshd.KeysDir,
			Samba:      func() bool { _, err := exec.LookPath("smbpasswd"); return err == nil }},
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

// taskBackend runs the task work through the helper socket.
type taskBackend struct{ c *helper.Client }

func helperMsg(err error) error {
	if he, ok := helper.AsError(err); ok {
		return errors.New(he.Message)
	}
	return nil
}

func (b taskBackend) SmartTest(ctx context.Context, diskID, test string) (*int, error) {
	r, err := b.c.SmartTest(ctx, diskID, test)
	if err != nil {
		if he, ok := helper.AsError(err); ok && he.Code == helper.CodeTestRunning {
			return nil, tasks.ErrTestRunning
		}
		if m := helperMsg(err); m != nil {
			return nil, m
		}
		return nil, errors.New(tasks.MsgFailed)
	}
	return r.EstimatedMinutes, nil
}

func (b taskBackend) RecycleCleanup(ctx context.Context) (int, int64, error) {
	r, err := b.c.ShareRecycleCleanup(ctx)
	if err != nil {
		if m := helperMsg(err); m != nil {
			return 0, 0, m
		}
		return 0, 0, errors.New(tasks.MsgFailed)
	}
	return r.RemovedFiles, r.RemovedBytes, nil
}

func (b taskBackend) PowerBlockers(ctx context.Context) ([]string, error) {
	pf, err := b.c.PowerPreflight(ctx)
	if err != nil {
		if m := helperMsg(err); m != nil {
			return nil, m
		}
		return nil, errors.New(tasks.MsgPowerFailed)
	}
	codes := make([]string, 0, len(pf.Blockers))
	for _, x := range pf.Blockers {
		codes = append(codes, x.Code)
	}
	return codes, nil
}

func (b taskBackend) Power(ctx context.Context, action, user string) error {
	_, err := b.c.PowerAction(ctx, action, false, user)
	if err == nil {
		return nil
	}
	if he, ok := helper.AsError(err); ok && he.Code == helper.CodeBlocked {
		return tasks.ErrPowerBlocked
	}
	if m := helperMsg(err); m != nil {
		return m
	}
	return errors.New(tasks.MsgPowerFailed)
}

// runTask is the ExecStart of nas-revival-task@.service: it runs one task and
// exits 1 unless it succeeded or was skipped.
func runTask(args []string) error {
	fl := flag.NewFlagSet("task run", flag.ContinueOnError)
	configDir := fl.String("config", "/etc/nas-revival", "configuration directory (tasks.json)")
	stateDir := fl.String("state", "/var/lib/nas-revival", "state directory (task history)")
	helperSock := fl.String("helper-socket", "/run/nas-revival-helper/helper.sock", "privileged helper socket")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 || !tasks.ValidID(fl.Arg(0)) {
		return errors.New("usage: nas-revival task run [-config D] [-state D] <id>")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	r := tasks.NewRunner(filepath.Join(*configDir, "tasks.json"), filepath.Join(*stateDir, "task-history.json"),
		taskBackend{c: &helper.Client{Socket: *helperSock}})
	r.Backup = tasks.NewBackupWorker(backupVolumeMounted)
	run, err := r.Run(ctx, fl.Arg(0))
	if err != nil {
		return err
	}
	if run.Result == tasks.ResultFailed || run.Result == tasks.ResultStopped {
		return fmt.Errorf("task %s: %s: %s", fl.Arg(0), run.Result, run.Message)
	}
	return nil
}

// newShareManager is the share manager of the helper and of the root
// subcommands that change shares' folders.
func newShareManager() *helper.ShareManager {
	return &helper.ShareManager{Path: shares.DefaultConfigPath,
		Accounts:  accounts.Paths{Passwd: accounts.SystemPaths.Passwd, Group: accounts.SystemPaths.Group},
		Mountinfo: "/proc/self/mountinfo", Statfs: shares.SysStatfs, Probe: shares.NewACLProbe(shares.SysSetxattr),
		SetXattr: shares.SysSetxattr, Chown: shares.SysLchown, Run: helper.ExecRunner, LookPath: exec.LookPath,
		SambaDir: "/etc/samba", SambaConf: shares.SambaConfPath, SambaMain: "/etc/samba/smb.conf", Now: time.Now}
}

// backupVolumeMounted checks that the backup volume is a mounted data volume.
func backupVolumeMounted() bool {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer f.Close()
	for _, m := range shares.DataMounts(shares.ParseMountinfo(f)) {
		if m.Point == tasks.BackupVolume {
			return true
		}
	}
	return false
}

// importPull merges the jobs of the interim nas-pull timer into tasks.json as
// backup-pull tasks (root only; same validation and timers as task.set).
func importPull(args []string) error {
	fl := flag.NewFlagSet("task import-pull", flag.ContinueOnError)
	at := fl.String("time", tasks.DefaultImportTime, "daily start (HH:MM, NAS local time)")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return errors.New("usage: nas-revival task import-pull [-time HH:MM] <jobs.conf>")
	}
	if os.Geteuid() != 0 {
		return errors.New("task import-pull must run as root")
	}
	f, err := os.Open(fl.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	existing, err := tasks.Load(tasks.DefaultConfigPath)
	if err != nil {
		return err
	}
	merged, err := tasks.ImportPull(f, existing, *at)
	if err != nil {
		return err
	}
	// No SharesPath: imported jobs have no destination yet (task migrate-backup-dest gives it).
	tkm := &helper.TaskManager{Path: tasks.DefaultConfigPath, UnitDir: "/etc/systemd/system", Run: helper.ExecRunner}
	res, err := tkm.Set(context.Background(), merged, "import-pull")
	if err != nil {
		return err
	}
	fmt.Printf("tasks.json: %d tasks\n", len(res.Tasks))
	return nil
}

// migrateBackupDest is the one-time move of the backup data from the fixed
// /srv/data/apps and /srv/data/versions to a backupTarget share, and the
// destination of every backup task that has none (root only).
func migrateBackupDest(args []string) error {
	fl := flag.NewFlagSet("task migrate-backup-dest", flag.ContinueOnError)
	share := fl.String("share", "", "backupTarget share that receives the backups")
	folder := fl.String("folder", "daily-backup", "subfolder of the share (one level, may be empty)")
	dry := fl.Bool("n", false, "only print the plan")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if *share == "" || fl.NArg() != 0 {
		return errors.New("usage: nas-revival task migrate-backup-dest -share NAME [-folder NAME] [-n]")
	}
	if os.Geteuid() != 0 {
		return errors.New("task migrate-backup-dest must run as root")
	}
	uid, gid, err := helper.LookupUser(tasks.BackupUser)
	if err != nil {
		return fmt.Errorf("account %s: %w", tasks.BackupUser, err)
	}
	list, err := tasks.Load(tasks.DefaultConfigPath)
	if err != nil {
		return err
	}
	plan, err := tasks.PlanMigration(tasks.BackupVolume, shares.DefaultConfigPath, list,
		tasks.BackupDest{Share: *share, Folder: *folder})
	if err != nil {
		return err
	}
	if len(plan.Migrated) == 0 {
		fmt.Println("no backup task without a destination, nothing to do")
		return nil
	}
	fmt.Printf("tasks that get the destination %s/%s: %s\n", *share, *folder, strings.Join(plan.Migrated, ", "))
	for _, d := range plan.Mkdirs {
		fmt.Println("mkdir  ", d)
	}
	for _, mv := range plan.Moves {
		fmt.Printf("move    %s -> %s\n", mv.From, mv.To)
	}
	if *dry {
		fmt.Println("dry run: nothing changed")
		return nil
	}
	if !backupVolumeMounted() {
		return errors.New(tasks.MsgNoVolume)
	}
	if err := plan.Apply(func(p string) error { return os.Lchown(p, int(uid), gid) }); err != nil {
		return fmt.Errorf("move failed (run again to continue): %w", err)
	}
	for _, p := range tasks.RemoveLegacyDirs(tasks.BackupVolume) {
		fmt.Println("removed", p)
	}
	ctx := context.Background()
	if err := newShareManager().Reapply(ctx, *share); err != nil {
		return fmt.Errorf("permissions of share %s: %w (data moved, tasks.json not changed; run again)", *share, err)
	}
	tkm := &helper.TaskManager{Path: tasks.DefaultConfigPath, UnitDir: "/etc/systemd/system", Run: helper.ExecRunner,
		SharesPath: shares.DefaultConfigPath}
	res, err := tkm.Set(ctx, plan.Tasks, "migrate-backup-dest")
	if err != nil {
		return fmt.Errorf("tasks.json: %w (data moved; run again)", err)
	}
	fmt.Printf("tasks.json: %d tasks, %d migrated\n", len(res.Tasks), len(plan.Migrated))
	return nil
}
