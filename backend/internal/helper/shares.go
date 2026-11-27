package helper

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/shares"
)

// Enforcement modes of a share.
const (
	EnforceACL = "acl" // POSIX ACLs on the folder
	EnforceSMB = "smb" // the filesystem has no ACLs, Samba enforces
)

// ShareInfo is a share with its derived state.
type ShareInfo struct {
	shares.Share
	Path        string              `json:"path"`
	Exists      bool                `json:"exists"`
	Enforcement string              `json:"enforcement"`
	Effective   []shares.UserAccess `json:"effective"`
	Conflicts   []string            `json:"conflicts"`
}

// SmbState reports the Samba integration.
type SmbState struct {
	Installed bool `json:"installed"`
	Included  bool `json:"included"`
}

type SharePrincipals struct {
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}

// ShareList is the result of every share operation.
type ShareList struct {
	Shares     []ShareInfo     `json:"shares"`
	Volumes    []shares.Volume `json:"volumes"`
	Smb        SmbState        `json:"smb"`
	Principals SharePrincipals `json:"principals"`
}

// ShareDeleteResult is the result of share.delete.
type ShareDeleteResult struct {
	ShareList
	DataRemoval string `json:"dataRemoval"` // "started" | "none"
}

// ShareDelete is the argument of share.delete.
type ShareDelete struct {
	Name        string `json:"name"`
	DeleteData  bool   `json:"deleteData"`
	ConfirmName string `json:"confirmName"`
}

// ShareManager owns shares.json, the folders and the Samba fragment.
type ShareManager struct {
	Path      string // shares.json
	Accounts  accounts.Paths
	Mountinfo string
	Statfs    shares.Statfs
	Probe     shares.ACLProbe
	SetXattr  shares.XattrSetter
	Chown     shares.ChownFunc
	Run       Runner
	LookPath  func(string) (string, error)
	SambaDir  string // /etc/samba
	SambaConf string // generated include file
	SambaMain string // smb.conf, only read
	Now       func() time.Time
	// Background runs the slow folder removal; tests run it synchronously.
	Background func(func())
	// NfsExported blocks deleting a share that NFS still exports; NfsRefresh
	// re-renders the exports after a change. Both are nil without NFS.
	NfsExported func(name string) bool
	NfsRefresh  func(ctx context.Context, cfg shares.Config)
	// InUseAsBackupTarget names what still uses a share as a backup
	// destination (a task, the USB copy); nil means nothing does. It blocks
	// turning backupTarget off, renaming and deleting the share.
	InUseAsBackupTarget func(share string) (user string, inUse bool)
	// BackupVolume is the only volume a backupTarget share may live on (the
	// backup unit may write only there).
	BackupVolume string
	mu           sync.Mutex
}

func (m *ShareManager) nfsRefresh(ctx context.Context, cfg shares.Config) {
	if m.NfsRefresh != nil {
		m.NfsRefresh(ctx, cfg)
	}
}

// ValidateShare checks the shape of a share (the web process runs it too).
func ValidateShare(s shares.Share) (shares.Share, error) {
	n, err := shares.ValidateShape(s)
	return n, shareErr(err)
}

func shareErr(err error) error {
	var fe *shares.FieldError
	if errors.As(err, &fe) {
		return invalid(fe.Field, fe.Msg)
	}
	return err
}

func (m *ShareManager) loadDB() (accounts.DB, error) {
	db, err := accounts.Load(m.Accounts)
	if err != nil {
		slog.Error("helper: read accounts", "err", err)
		return db, internalErr("Nelze načíst uživatelské účty.")
	}
	return db, nil
}

func (m *ShareManager) loadCfg() (shares.Config, error) {
	c, err := shares.Load(m.Path)
	if err != nil {
		slog.Error("helper: read shares.json", "err", err)
		return c, internalErr("Konfiguraci sdílených složek nelze načíst.")
	}
	return c, nil
}

func (m *ShareManager) saveCfg(c shares.Config) error {
	if err := shares.Save(m.Path, c); err != nil {
		slog.Error("helper: write shares.json", "err", err)
		return internalErr("Konfiguraci sdílených složek nelze uložit.")
	}
	return nil
}

func (m *ShareManager) volumes() []shares.Volume {
	v, err := shares.Volumes(m.Mountinfo, m.Statfs, m.Probe)
	if err != nil {
		slog.Error("helper: mountinfo", "err", err)
		return []shares.Volume{}
	}
	return v
}

func (m *ShareManager) hasACL(vol string) bool { return m.Probe(vol) }

// LoadConfig and LoadDB give the File Station the shares and accounts.
func (m *ShareManager) LoadConfig() (shares.Config, error) { return m.loadCfg() }

func (m *ShareManager) LoadDB() (accounts.DB, error) { return m.loadDB() }

func (m *ShareManager) smbState() SmbState {
	st := SmbState{}
	if _, err := m.LookPath("smbd"); err == nil {
		st.Installed = true
	}
	if b, err := os.ReadFile(m.SambaMain); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.Join(strings.Fields(l), " ") == shares.SambaInclude {
				st.Included = true
			}
		}
	}
	return st
}

func (m *ShareManager) List(ctx context.Context) (ShareList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list()
}

func (m *ShareManager) list() (ShareList, error) {
	db, err := m.loadDB()
	if err != nil {
		return ShareList{}, err
	}
	cfg, err := m.loadCfg()
	if err != nil {
		return ShareList{}, err
	}
	out := ShareList{Shares: []ShareInfo{}, Volumes: m.volumes(), Smb: m.smbState(),
		Principals: SharePrincipals{Users: []string{}, Groups: []string{}}}
	for _, s := range cfg.Shares {
		info := ShareInfo{Share: s, Path: s.Path(), Enforcement: EnforceSMB,
			Effective: shares.Effective(s, db), Conflicts: shares.Conflicts(s, db)}
		if m.hasACL(s.Volume) {
			info.Enforcement = EnforceACL
		}
		if fi, err := os.Lstat(s.Path()); err == nil && fi.IsDir() {
			info.Exists = true
		}
		out.Shares = append(out.Shares, info)
	}
	for _, u := range db.NASUsers() {
		out.Principals.Users = append(out.Principals.Users, u.Name)
	}
	out.Principals.Groups = append(out.Principals.Groups, accounts.UsersGroup)
	if _, ok := db.Group(accounts.NfsGroup); ok {
		out.Principals.Groups = append(out.Principals.Groups, accounts.NfsGroup)
	}
	for _, g := range db.Groups {
		if db.IsEditableGroup(g.Name) {
			out.Principals.Groups = append(out.Principals.Groups, g.Name)
		}
	}
	sort.Strings(out.Principals.Groups)
	return out, nil
}

func volumeMissing(v string) error {
	return invalid("volume", "Svazek "+v+" není připojený; složka by vznikla na systémovém disku.")
}

// applyFS gives a folder its owner, mode and (in ACL mode) ACLs.
func (m *ShareManager) applyFS(ctx context.Context, s shares.Share, db accounts.DB, vol shares.Volume, adopted bool) error {
	path := s.Path()
	// FAT has no owners or modes: the USB mount options give root:nas-users
	// 0770/0660 to everything (W4.2), Samba enforces the rest.
	if vol.FsType == "vfat" {
		return nil
	}
	if vol.ACL {
		if err := shares.SetupRootACL(path, m.Chown); err != nil {
			slog.Error("helper: share root", "path", path, "err", err)
			return internalErr("Složku se nepodařilo nastavit.")
		}
		n, err := shares.ApplyTree(ctx, path, s, db, m.SetXattr)
		return aclErr(n, err)
	}
	g, ok := db.Group(accounts.UsersGroup)
	if !ok {
		return internalErr("Skupina nas-users neexistuje.")
	}
	if err := shares.SetupRootFallback(path, g.GID, m.Chown); err != nil {
		slog.Error("helper: share root", "path", path, "err", err)
		return internalErr("Složku se nepodařilo nastavit.")
	}
	if adopted {
		n, err := shares.AdoptFallback(ctx, path, g.GID, m.Chown)
		return aclErr(n, err)
	}
	return nil
}

func aclErr(n int, err error) error {
	var inc *shares.IncompleteError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, shares.ErrNoACL):
		return internalErr("Svazek nepodporuje ACL.")
	case errors.As(err, &inc), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return internalErr(fmt.Sprintf("Oprávnění se nepodařilo použít na všechny položky (zpracováno %d).", n))
	}
	slog.Error("helper: apply permissions", "err", err)
	return internalErr("Oprávnění se nepodařilo použít.")
}

// writeSamba renders the fragment, validates it with testparm and reloads
// smbd. Without /etc/samba nothing is written.
func (m *ShareManager) writeSamba(ctx context.Context, cfg shares.Config) error {
	if fi, err := os.Stat(m.SambaDir); err != nil || !fi.IsDir() {
		return nil
	}
	text := shares.Render(cfg.Shares, m.hasACL)
	if _, err := m.LookPath("testparm"); err == nil {
		tmp, err := os.CreateTemp("", "nas-revival-smb-*.conf")
		if err != nil {
			return internalErr("Konfiguraci Samby nelze ověřit.")
		}
		defer os.Remove(tmp.Name())
		_, werr := tmp.WriteString("[global]\n" + text)
		tmp.Close()
		if werr != nil {
			return internalErr("Konfiguraci Samby nelze ověřit.")
		}
		if out, err := m.Run(ctx, "testparm", "-s", tmp.Name()); err != nil {
			slog.Error("helper: testparm rejected shares", "err", err, "out", strings.TrimSpace(string(out)))
			return internalErr("Konfigurace Samby je neplatná; nic se nezměnilo.")
		}
	}
	if err := atomicfile.Write(m.SambaConf, []byte(text), 0o644); err != nil {
		slog.Error("helper: write samba fragment", "err", err)
		return internalErr("Konfiguraci Samby nelze zapsat.")
	}
	if _, err := m.LookPath("smbd"); err == nil {
		if _, err := m.Run(ctx, "systemctl", "is-active", "--quiet", "smbd"); err == nil {
			if out, err := m.Run(ctx, "smbcontrol", "smbd", "reload-config"); err != nil {
				slog.Warn("helper: smbcontrol reload-config failed", "err", err, "out", strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

// checkNew runs the checks that need the volume list and account database.
func (m *ShareManager) checkVolume(s shares.Share) (shares.Volume, error) {
	v, err := shares.FindVolume(m.volumes(), s.Volume)
	if err != nil {
		return v, volumeMissing(s.Volume)
	}
	return v, nil
}

// checkBackupTarget adds the rules of a backup destination: the volume the
// backup unit may write to, and ACLs (the runner gets its access that way).
func (m *ShareManager) checkBackupTarget(s shares.Share, vol shares.Volume) error {
	if !s.BackupTarget {
		return nil
	}
	if m.BackupVolume == "" || s.Volume != m.BackupVolume {
		return invalid("backupTarget", "Cíl záloh musí být na datovém svazku "+m.BackupVolume+".")
	}
	if !vol.ACL {
		return invalid("backupTarget", "Cíl záloh potřebuje svazek s ACL.")
	}
	return nil
}

// backupTargetInUse refuses a change that would pull a folder from under a
// backup that writes into it.
func (m *ShareManager) backupTargetInUse(s shares.Share, what string) error {
	if !s.BackupTarget || m.InUseAsBackupTarget == nil {
		return nil
	}
	if user, used := m.InUseAsBackupTarget(s.Name); used {
		return invalid("", "Složku používá "+user+" jako cíl záloh; "+what+" nejdřív změňte tam.")
	}
	return nil
}

func (m *ShareManager) Create(ctx context.Context, in shares.Share, actor string) (ShareList, error) {
	s, err := ValidateShare(in)
	if err != nil {
		return ShareList{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.loadDB()
	if err != nil {
		return ShareList{}, err
	}
	if err := shareErr(shares.CheckPrincipals(s, db)); err != nil {
		return ShareList{}, err
	}
	cfg, err := m.loadCfg()
	if err != nil {
		return ShareList{}, err
	}
	if cfg.Find(s.Name) >= 0 {
		return ShareList{}, invalid("name", "Sdílená složka s tímto názvem už existuje.")
	}
	if len(cfg.Shares) >= shares.MaxShares {
		return ShareList{}, invalid("name", "Dosáhli jste nejvyššího počtu sdílených složek.")
	}
	for _, o := range cfg.Shares {
		if o.Volume == s.Volume && strings.EqualFold(o.Folder, s.Folder) {
			return ShareList{}, invalid("folder", "Tuto složku už používá jiná sdílená složka.")
		}
	}
	vol, err := m.checkVolume(s)
	if err != nil {
		return ShareList{}, err
	}
	if err := m.checkBackupTarget(s, vol); err != nil {
		return ShareList{}, err
	}
	adopted, err := shares.PrepareDir(s.Path())
	if err != nil {
		slog.Error("helper: prepare share folder", "path", s.Path(), "err", err)
		return ShareList{}, invalid("folder", "Složku nelze vytvořit nebo převzít (existuje a není to běžná složka?).")
	}
	cleanup := func() {
		if !adopted {
			os.Remove(s.Path())
		}
	}
	if err := m.applyFS(ctx, s, db, vol, adopted); err != nil {
		cleanup()
		return ShareList{}, err
	}
	old := cfg
	cfg.Shares = append(append([]shares.Share{}, cfg.Shares...), s)
	if err := m.saveCfg(cfg); err != nil {
		cleanup()
		return ShareList{}, err
	}
	if err := m.writeSamba(ctx, cfg); err != nil {
		m.saveCfg(old)
		cleanup()
		return ShareList{}, err
	}
	slog.Info("helper: share.create", "share", s.Name, "path", s.Path(), "adopted", adopted, "enforcement", enforcement(vol.ACL), "user", actor)
	return m.list()
}

func enforcement(acl bool) string {
	if acl {
		return EnforceACL
	}
	return EnforceSMB
}

func (m *ShareManager) Update(ctx context.Context, in shares.Share, actor string) (ShareList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.loadCfg()
	if err != nil {
		return ShareList{}, err
	}
	idx := cfg.Find(in.Name)
	if idx < 0 {
		return ShareList{}, &Error{Code: CodeNotFound, Message: "Sdílená složka neexistuje."}
	}
	cur := cfg.Shares[idx]
	if in.Volume == "" {
		in.Volume = cur.Volume
	}
	if in.Folder == "" {
		in.Folder = cur.Folder
	}
	s, err := ValidateShare(in)
	if err != nil {
		return ShareList{}, err
	}
	if s.Name != cur.Name || s.Volume != cur.Volume || s.Folder != cur.Folder {
		return ShareList{}, invalid("", "Název, svazek ani složku sdílené složky nelze změnit.")
	}
	db, err := m.loadDB()
	if err != nil {
		return ShareList{}, err
	}
	if err := shareErr(shares.CheckPrincipals(s, db)); err != nil {
		return ShareList{}, err
	}
	vol, err := m.checkVolume(s)
	if err != nil {
		return ShareList{}, err
	}
	if err := m.checkBackupTarget(s, vol); err != nil {
		return ShareList{}, err
	}
	if s.BackupTarget && !cur.BackupTarget && m.NfsExported != nil && m.NfsExported(s.Name) {
		return ShareList{}, invalid("backupTarget", "Cíl záloh nelze exportovat přes NFS; nejdřív odeberte export ve Službách → NFS.")
	}
	if cur.BackupTarget && !s.BackupTarget {
		if err := m.backupTargetInUse(cur, "volbu Cíl záloh"); err != nil {
			return ShareList{}, err
		}
	}
	if fi, err := os.Lstat(s.Path()); err == nil && fi.IsDir() {
		if err := m.applyFS(ctx, s, db, vol, false); err != nil {
			return ShareList{}, err
		}
	}
	old := cfg
	cfg.Shares = append([]shares.Share{}, cfg.Shares...)
	cfg.Shares[idx] = s
	if err := m.saveCfg(cfg); err != nil {
		return ShareList{}, err
	}
	if err := m.writeSamba(ctx, cfg); err != nil {
		m.saveCfg(old)
		return ShareList{}, err
	}
	m.nfsRefresh(ctx, cfg)
	slog.Info("helper: share.update", "share", s.Name, "user", actor)
	return m.list()
}

func (m *ShareManager) Delete(ctx context.Context, d ShareDelete, actor string) (ShareDeleteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.loadCfg()
	if err != nil {
		return ShareDeleteResult{}, err
	}
	idx := cfg.Find(d.Name)
	if idx < 0 {
		return ShareDeleteResult{}, &Error{Code: CodeNotFound, Message: "Sdílená složka neexistuje."}
	}
	s := cfg.Shares[idx]
	res := ShareDeleteResult{DataRemoval: "none"}
	if m.NfsExported != nil && m.NfsExported(s.Name) {
		return res, invalid("", "Složka je exportovaná přes NFS; nejdřív ji odeberte ve Službách → NFS.")
	}
	if err := m.backupTargetInUse(s, "cíl"); err != nil {
		return res, err
	}
	var trash string
	if d.DeleteData {
		if d.ConfirmName != s.Name {
			return res, invalid("confirmName", "Pro smazání dat napište přesný název složky.")
		}
		if _, err := m.checkVolume(s); err != nil {
			return res, err
		}
		fi, err := os.Lstat(s.Path())
		switch {
		case err == nil && fi.IsDir():
			trash = fmt.Sprintf("%s/.nas-revival-deleted-%s-%d", s.Volume, s.Folder, m.Now().Unix())
			if err := os.Rename(s.Path(), trash); err != nil {
				slog.Error("helper: share.delete rename", "err", err)
				return res, internalErr("Složku se nepodařilo odebrat; nic se nezměnilo.")
			}
		case err == nil:
			return res, invalid("", "Složka není běžná složka; data se nesmažou.")
		}
	}
	old := cfg
	cfg.Shares = append(append([]shares.Share{}, cfg.Shares[:idx]...), cfg.Shares[idx+1:]...)
	fail := func(err error) (ShareDeleteResult, error) {
		if trash != "" {
			os.Rename(trash, s.Path())
		}
		return res, err
	}
	if err := m.saveCfg(cfg); err != nil {
		return fail(err)
	}
	if err := m.writeSamba(ctx, cfg); err != nil {
		m.saveCfg(old)
		return fail(err)
	}
	if trash != "" {
		res.DataRemoval = "started"
		bg := m.Background
		if bg == nil {
			bg = func(f func()) { go f() }
		}
		bg(func() {
			if err := os.RemoveAll(trash); err != nil {
				slog.Error("helper: share data removal failed", "share", s.Name, "err", err)
				return
			}
			slog.Info("helper: share data removed", "share", s.Name)
		})
	}
	slog.Info("helper: share.delete", "share", s.Name, "deleteData", d.DeleteData, "user", actor)
	l, err := m.list()
	res.ShareList = l
	return res, err
}

// RecycleCleanup empties the expired part of every recycle bin.
func (m *ShareManager) RecycleCleanup(ctx context.Context, actor string) (shares.RecycleResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.loadCfg()
	if err != nil {
		return shares.RecycleResult{}, err
	}
	var total shares.RecycleResult
	for _, s := range cfg.Shares {
		if !s.Recycle.Enabled {
			continue
		}
		if _, err := m.checkVolume(s); err != nil {
			continue
		}
		r, err := shares.CleanupRecycle(ctx, s.Path(), s.Recycle.Days, m.Now())
		total.RemovedFiles += r.RemovedFiles
		total.RemovedBytes += r.RemovedBytes
		if err != nil {
			return total, internalErr("Vysypání koše bylo přerušeno.")
		}
	}
	slog.Info("helper: share.recycleCleanup", "files", total.RemovedFiles, "bytes", total.RemovedBytes, "user", actor)
	return total, nil
}

// Restore writes an older shares.json back (config rollback), re-applies the
// permissions of the folders that exist and re-renders Samba. It neither
// creates nor deletes folders.
func (m *ShareManager) Restore(ctx context.Context, data []byte, present bool) error {
	cfg := shares.Config{Version: 1, Shares: []shares.Share{}}
	if present {
		c, err := shares.Parse(data)
		if err != nil {
			return invalid("", "Uložená verze sdílených složek není platná.")
		}
		cfg = c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if present {
		if err := atomicfile.Write(m.Path, data, 0o644); err != nil {
			return internalErr("Konfiguraci sdílených složek nelze zapsat.")
		}
	} else if err := os.Remove(m.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return internalErr("Konfiguraci sdílených složek nelze odstranit.")
	}
	if db, err := m.loadDB(); err == nil {
		vols := m.volumes()
		for _, s := range cfg.Shares {
			v, err := shares.FindVolume(vols, s.Volume)
			if err != nil {
				continue
			}
			if fi, err := os.Lstat(s.Path()); err != nil || !fi.IsDir() {
				continue
			}
			if err := m.applyFS(ctx, s, db, v, false); err != nil {
				slog.Warn("helper: share restore", "share", s.Name, "err", err)
			}
		}
	}
	if err := m.writeSamba(ctx, cfg); err != nil {
		return err
	}
	m.nfsRefresh(ctx, cfg)
	return nil
}
