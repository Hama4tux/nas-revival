package helper

import (
	"context"
	"log/slog"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"nas-revival/backend/internal/accounts"
)

// Password limits: the same minimum as `nas-revival set-password`.
const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
	maxFullName    = 64
	maxGroupsPer   = 32
)

// StdinRunner runs a fixed command with data on stdin (passwords never go
// into argv, where other processes could read them).
type StdinRunner func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)

// ExecStdinRunner is the production StdinRunner.
func ExecStdinRunner(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(string(stdin))
	return cmd.Output()
}

// ValidName checks a new user or group name: lower-case letter first, then
// letters, digits, '_' or '-', at most 32 characters (useradd NAME_REGEX).
func ValidName(s string) bool {
	if s == "" || len(s) > 32 || !(s[0] >= 'a' && s[0] <= 'z') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// ValidateFullName returns the trimmed GECOS name; ':' and ',' would break
// the passwd format.
func ValidateFullName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxFullName || !utf8.ValidString(s) {
		return "", invalid("fullName", "Celé jméno může mít nejvýše 64 znaků.")
	}
	for _, r := range s {
		if r == ':' || r == ',' || unicode.IsControl(r) {
			return "", invalid("fullName", "Celé jméno nesmí obsahovat dvojtečku, čárku ani řídicí znaky.")
		}
	}
	return s, nil
}

// ValidatePassword: 12–256 characters, no control characters (chpasswd is
// line based, a newline would start a new record).
func ValidatePassword(pw string) error {
	if !utf8.ValidString(pw) || utf8.RuneCountInString(pw) < MinPasswordLen {
		return invalid("password", "Heslo musí mít alespoň 12 znaků.")
	}
	if len(pw) > MaxPasswordLen {
		return invalid("password", "Heslo může mít nejvýše 256 bajtů.")
	}
	for _, r := range pw {
		if unicode.IsControl(r) {
			return invalid("password", "Heslo nesmí obsahovat řídicí znaky.")
		}
	}
	return nil
}

func validNameList(field string, list []string) ([]string, error) {
	if len(list) > maxGroupsPer {
		return nil, invalid(field, "Seznam je příliš dlouhý.")
	}
	out := []string{}
	seen := map[string]bool{}
	for _, n := range list {
		if !ValidName(n) {
			return nil, invalid(field, "Neplatné jméno: "+quoteShort(n))
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// UserCreate is the argument of user.create.
type UserCreate struct {
	Name     string   `json:"name"`
	FullName string   `json:"fullName"`
	Password string   `json:"password"`
	Groups   []string `json:"groups"`
}

// ValidateUserCreate checks the shape of the request (the web process runs
// it too, so typos fail before the helper is asked).
func ValidateUserCreate(c UserCreate) (UserCreate, error) {
	if !ValidName(c.Name) {
		return c, invalid("name", "Jméno: malá písmena bez diakritiky, číslice, _ nebo -, začíná písmenem, nejvýše 32 znaků.")
	}
	if accounts.Reserved(c.Name) {
		return c, invalid("name", "Jména začínající nas- jsou vyhrazená.")
	}
	var err error
	if c.FullName, err = ValidateFullName(c.FullName); err != nil {
		return c, err
	}
	if c.Groups, err = validNameList("groups", c.Groups); err != nil {
		return c, err
	}
	return c, ValidatePassword(c.Password)
}

// SambaState of a user's password.
const (
	SambaSet         = "set"
	SambaMissing     = "missing"
	SambaUnavailable = "unavailable" // Samba is not installed
)

type UserInfo struct {
	Name     string   `json:"name"`
	UID      int      `json:"uid"`
	FullName string   `json:"fullName"`
	Groups   []string `json:"groups"`
	Disabled bool     `json:"disabled"`
	Password struct {
		Linux bool   `json:"linux"`
		Samba string `json:"samba"`
		Web   bool   `json:"web"`
		// ChangedOutside: the Linux password differs from the one set with
		// the web hash (e.g. `passwd` on the console); set it again.
		ChangedOutside bool   `json:"changedOutside"`
		Changed        string `json:"changed,omitempty"`
	} `json:"password"`
}

type GroupInfo struct {
	Name    string   `json:"name"`
	GID     int      `json:"gid"`
	Members []string `json:"members"`
	// Builtin groups (nas-users) are shown read-only.
	Builtin bool `json:"builtin"`
}

type UserList struct {
	Users  []UserInfo  `json:"users"`
	Groups []GroupInfo `json:"groups"`
	Samba  bool        `json:"samba"`
}

// UserManager changes NAS accounts with the shadow tools (useradd, usermod,
// chpasswd, gpasswd …) and keeps the web and Samba passwords in step.
type UserManager struct {
	Paths    accounts.Paths
	Web      accounts.WebPasswords
	Run      Runner
	RunStdin StdinRunner
	// Samba reports whether smbpasswd is installed.
	Samba func() bool
	Hash  func(password string) (string, error)
	Now   func() time.Time
	mu    sync.Mutex
}

func (m *UserManager) load() (accounts.DB, error) {
	db, err := accounts.Load(m.Paths)
	if err != nil {
		slog.Error("helper: read accounts", "err", err)
		return db, internalErr("Nelze načíst uživatelské účty.")
	}
	return db, nil
}

func (m *UserManager) today() int { return int(m.Now().Unix() / 86400) }

// sambaUsers lists users with a Samba password; nil when Samba is absent.
func (m *UserManager) sambaUsers(ctx context.Context) map[string]bool {
	if m.Samba == nil || !m.Samba() {
		return nil
	}
	out := map[string]bool{}
	b, err := m.Run(ctx, "pdbedit", "-L")
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		if n, _, ok := strings.Cut(l, ":"); ok {
			out[n] = true
		}
	}
	return out
}

func (m *UserManager) List(ctx context.Context) (UserList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list(ctx)
}

func (m *UserManager) list(ctx context.Context) (UserList, error) {
	db, err := m.load()
	if err != nil {
		return UserList{}, err
	}
	web, err := m.Web.Load()
	if err != nil {
		slog.Error("helper: read web passwords", "err", err)
		web = map[string]accounts.WebPassword{}
	}
	smb := m.sambaUsers(ctx)
	out := UserList{Users: []UserInfo{}, Groups: []GroupInfo{}, Samba: smb != nil}
	today := m.today()
	for _, u := range db.NASUsers() {
		sh := db.Shadow[u.Name]
		info := UserInfo{Name: u.Name, UID: u.UID, FullName: accounts.FullName(u.Gecos),
			Groups: db.EditableGroupsOf(u.Name), Disabled: sh.Locked(today)}
		info.Password.Linux = sh.HasPassword()
		w, ok := web[u.Name]
		info.Password.Web = ok && w.Hash != ""
		if ok {
			info.Password.Changed = w.Changed
			info.Password.ChangedOutside = info.Password.Linux && w.Shadow != sh.Fingerprint()
		}
		switch {
		case smb == nil:
			info.Password.Samba = SambaUnavailable
		case smb[u.Name]:
			info.Password.Samba = SambaSet
		default:
			info.Password.Samba = SambaMissing
		}
		out.Users = append(out.Users, info)
	}
	for _, g := range db.Groups {
		builtin := g.Name == accounts.UsersGroup
		if !builtin && !db.IsEditableGroup(g.Name) {
			continue
		}
		gi := GroupInfo{Name: g.Name, GID: g.GID, Members: []string{}, Builtin: builtin}
		if builtin {
			for _, u := range db.NASUsers() {
				gi.Members = append(gi.Members, u.Name)
			}
		} else {
			for _, n := range g.Members {
				if db.IsNASUser(n) {
					gi.Members = append(gi.Members, n)
				}
			}
			sort.Strings(gi.Members)
		}
		out.Groups = append(out.Groups, gi)
	}
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Name < out.Groups[j].Name })
	return out, nil
}

// run executes one shadow-tool step and turns a failure into a Czech error.
func (m *UserManager) run(ctx context.Context, msg string, name string, args ...string) error {
	if out, err := m.Run(ctx, name, args...); err != nil {
		slog.Error("helper: "+name+" failed", "err", err, "out", strings.TrimSpace(string(out)))
		return internalErr(msg)
	}
	return nil
}

func (m *UserManager) nasUser(db accounts.DB, name string) error {
	if !ValidName(name) || !db.IsNASUser(name) {
		return &Error{Code: CodeNotFound, Message: "Uživatel neexistuje nebo ho nelze spravovat."}
	}
	return nil
}

// checkGroups: every requested group must exist and be editable (never
// sudo, disk, adm …).
func checkGroups(db accounts.DB, groups []string) error {
	for _, g := range groups {
		if !db.IsEditableGroup(g) {
			return invalid("groups", "Skupinu "+quoteShort(g)+" nelze přiřadit.")
		}
	}
	return nil
}

func (m *UserManager) ensureUsersGroup(ctx context.Context, db accounts.DB) error {
	if _, ok := db.Group(accounts.UsersGroup); ok {
		return nil
	}
	return m.run(ctx, "Nelze vytvořit skupinu nas-users.", "groupadd", "--system", accounts.UsersGroup)
}

func (m *UserManager) Create(ctx context.Context, c UserCreate, actor string) (UserList, error) {
	c, err := ValidateUserCreate(c)
	if err != nil {
		return UserList{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.load()
	if err != nil {
		return UserList{}, err
	}
	if _, ok := db.User(c.Name); ok {
		return UserList{}, invalid("name", "Uživatel s tímto jménem už existuje.")
	}
	if _, ok := db.Group(c.Name); ok {
		return UserList{}, invalid("name", "Toto jméno už používá skupina.")
	}
	if err := checkGroups(db, c.Groups); err != nil {
		return UserList{}, err
	}
	if err := m.ensureUsersGroup(ctx, db); err != nil {
		return UserList{}, err
	}
	// No home directory and no login shell: access is through shares (and
	// later SFTP), never an interactive shell.
	args := []string{"--no-create-home", "--no-user-group", "--gid", accounts.UsersGroup,
		"--home-dir", "/nonexistent", "--shell", "/usr/sbin/nologin", "--comment", c.FullName}
	if len(c.Groups) > 0 {
		args = append(args, "--groups", strings.Join(c.Groups, ","))
	}
	if err := m.run(ctx, "Uživatele se nepodařilo vytvořit.", "useradd", append(args, c.Name)...); err != nil {
		return UserList{}, err
	}
	if _, err := m.setPassword(ctx, c.Name, c.Password); err != nil {
		// Do not leave an account without a password behind.
		m.Run(ctx, "userdel", c.Name)
		m.forgetWeb(c.Name)
		return UserList{}, err
	}
	slog.Info("helper: user.create", "account", c.Name, "groups", c.Groups, "user", actor)
	return m.list(ctx)
}

// UserUpdate is the argument of user.update.
type UserUpdate struct {
	Name     string   `json:"name"`
	FullName string   `json:"fullName"`
	Groups   []string `json:"groups"`
}

func (m *UserManager) Update(ctx context.Context, u UserUpdate, actor string) (UserList, error) {
	full, err := ValidateFullName(u.FullName)
	if err != nil {
		return UserList{}, err
	}
	groups, err := validNameList("groups", u.Groups)
	if err != nil {
		return UserList{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.load()
	if err != nil {
		return UserList{}, err
	}
	if err := m.nasUser(db, u.Name); err != nil {
		return UserList{}, err
	}
	if err := checkGroups(db, groups); err != nil {
		return UserList{}, err
	}
	// usermod --groups replaces the whole list: keep memberships the UI
	// does not manage.
	all := append(db.OtherGroupsOf(u.Name), groups...)
	if err := m.run(ctx, "Uživatele se nepodařilo upravit.", "usermod", "--comment", full,
		"--groups", strings.Join(all, ","), u.Name); err != nil {
		return UserList{}, err
	}
	slog.Info("helper: user.update", "account", u.Name, "groups", groups, "user", actor)
	return m.list(ctx)
}

// PasswordResult is returned by user.setPassword.
type PasswordResult struct {
	Samba bool `json:"samba"` // the Samba password was set too
}

func (m *UserManager) SetPassword(ctx context.Context, name, pw, actor string) (PasswordResult, error) {
	if err := ValidatePassword(pw); err != nil {
		return PasswordResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.load()
	if err != nil {
		return PasswordResult{}, err
	}
	if err := m.nasUser(db, name); err != nil {
		return PasswordResult{}, err
	}
	res, err := m.setPassword(ctx, name, pw)
	if err == nil {
		slog.Info("helper: user.setPassword", "account", name, "samba", res.Samba, "user", actor)
	}
	return res, err
}

// setPassword writes the three hashes: web argon2id (computed first, it
// cannot half-fail), Linux shadow, Samba. A Samba failure restores the old
// shadow hash; a failure to store the web hash is reported with the services
// that already have the new password.
func (m *UserManager) setPassword(ctx context.Context, name, pw string) (PasswordResult, error) {
	var res PasswordResult
	hash, err := m.Hash(pw)
	if err != nil {
		return res, internalErr("Heslo se nepodařilo zpracovat.")
	}
	before, err := m.load()
	if err != nil {
		return res, err
	}
	old := before.Shadow[name]
	// A fresh useradd leaves a bare "!" (no password yet): that is not a
	// disabled account and must not be locked again after chpasswd.
	locked := old.Locked(m.today()) && (old.HasPassword() || old.Expire != "")
	if out, err := m.RunStdin(ctx, []byte(name+":"+pw+"\n"), "chpasswd"); err != nil {
		slog.Error("helper: chpasswd failed", "err", err, "out", strings.TrimSpace(string(out)))
		return res, internalErr("Heslo se nepodařilo nastavit (systémový účet). Nic se nezměnilo.")
	}
	// chpasswd drops the lock prefix of a disabled account.
	if locked && strings.HasPrefix(old.Hash, "!") {
		if err := m.run(ctx, "Heslo je změněné, ale účet se nepodařilo znovu zamknout.", "usermod", "--lock", name); err != nil {
			return res, err
		}
	}
	if m.Samba != nil && m.Samba() {
		if out, err := m.RunStdin(ctx, []byte(pw+"\n"+pw+"\n"), "smbpasswd", "-s", "-a", name); err != nil {
			slog.Error("helper: smbpasswd failed", "err", err, "out", strings.TrimSpace(string(out)))
			if old.HasPassword() {
				if _, rerr := m.RunStdin(ctx, []byte(name+":"+old.Hash+"\n"), "chpasswd", "--encrypted"); rerr == nil {
					return res, internalErr("Heslo se nepodařilo nastavit pro Sambu; heslo systému zůstalo původní.")
				}
			}
			return res, internalErr("Heslo se nepodařilo nastavit pro Sambu; systémový účet už má nové heslo, Samba a web původní.")
		}
		res.Samba = true
		if locked {
			m.Run(ctx, "smbpasswd", "-d", name)
		}
	}
	after, err := m.load()
	if err != nil {
		return res, internalErr("Heslo je změněné pro systém" + sambaToo(res) + ", ale ne pro webové rozhraní.")
	}
	web, err := m.Web.Load()
	if err == nil {
		web[name] = accounts.WebPassword{Hash: hash, Disabled: locked, Changed: m.Now().UTC().Format(time.RFC3339),
			Shadow: after.Shadow[name].Fingerprint()}
		err = m.Web.Save(web)
	}
	if err != nil {
		slog.Error("helper: write web passwords", "err", err)
		return res, internalErr("Heslo je změněné pro systém" + sambaToo(res) + ", ale ne pro webové rozhraní.")
	}
	return res, nil
}

func sambaToo(r PasswordResult) string {
	if r.Samba {
		return " a Sambu"
	}
	return ""
}

func (m *UserManager) forgetWeb(name string) error {
	web, err := m.Web.Load()
	if err != nil {
		return err
	}
	if _, ok := web[name]; !ok {
		return nil
	}
	delete(web, name)
	return m.Web.Save(web)
}

// SetDisabled locks or unlocks all three: shadow (lock + expiry, which also
// blocks SSH keys), Samba and the web flag.
func (m *UserManager) SetDisabled(ctx context.Context, name string, disabled bool, actor string) (UserList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.load()
	if err != nil {
		return UserList{}, err
	}
	if err := m.nasUser(db, name); err != nil {
		return UserList{}, err
	}
	smb := m.sambaUsers(ctx)
	if disabled {
		if err := m.run(ctx, "Účet se nepodařilo zakázat.", "usermod", "--lock", "--expiredate", "1", name); err != nil {
			return UserList{}, err
		}
		if smb[name] {
			if err := m.run(ctx, "Systémový účet je zakázaný, ale Samba ne.", "smbpasswd", "-d", name); err != nil {
				return UserList{}, err
			}
		}
	} else {
		args := []string{"--expiredate", ""}
		if strings.HasPrefix(db.Shadow[name].Hash, "!") && db.Shadow[name].HasPassword() {
			args = append(args, "--unlock")
		}
		if err := m.run(ctx, "Účet se nepodařilo povolit.", "usermod", append(args, name)...); err != nil {
			return UserList{}, err
		}
		if smb[name] {
			if err := m.run(ctx, "Systémový účet je povolený, ale Samba ne.", "smbpasswd", "-e", name); err != nil {
				return UserList{}, err
			}
		}
	}
	if web, err := m.Web.Load(); err == nil {
		if w, ok := web[name]; ok {
			w.Disabled = disabled
			web[name] = w
			err = m.Web.Save(web)
		}
		if err != nil {
			slog.Error("helper: write web passwords", "err", err)
			return UserList{}, internalErr("Systémový účet je změněný, ale webový ne.")
		}
	}
	slog.Info("helper: user.setDisabled", "account", name, "disabled", disabled, "user", actor)
	return m.list(ctx)
}

// Delete removes the account but keeps its files on the shares (they stay
// owned by the numeric uid).
func (m *UserManager) Delete(ctx context.Context, name, actor string) (UserList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.load()
	if err != nil {
		return UserList{}, err
	}
	if err := m.nasUser(db, name); err != nil {
		return UserList{}, err
	}
	if m.sambaUsers(ctx)[name] {
		if err := m.run(ctx, "Uživatele se nepodařilo odebrat ze Samby; nic se nesmazalo.", "smbpasswd", "-x", name); err != nil {
			return UserList{}, err
		}
	}
	if err := m.run(ctx, "Uživatele se nepodařilo smazat.", "userdel", name); err != nil {
		return UserList{}, err
	}
	if err := m.forgetWeb(name); err != nil {
		slog.Error("helper: write web passwords", "err", err)
		return UserList{}, internalErr("Účet je smazaný, ale webové heslo se nepodařilo odstranit.")
	}
	slog.Info("helper: user.delete", "account", name, "user", actor)
	return m.list(ctx)
}

func (m *UserManager) GroupCreate(ctx context.Context, name, actor string) (UserList, error) {
	if !ValidName(name) {
		return UserList{}, invalid("name", "Jméno: malá písmena bez diakritiky, číslice, _ nebo -, začíná písmenem, nejvýše 32 znaků.")
	}
	if accounts.Reserved(name) {
		return UserList{}, invalid("name", "Jména začínající nas- jsou vyhrazená.")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.load()
	if err != nil {
		return UserList{}, err
	}
	if _, ok := db.Group(name); ok {
		return UserList{}, invalid("name", "Skupina s tímto jménem už existuje.")
	}
	if _, ok := db.User(name); ok {
		return UserList{}, invalid("name", "Toto jméno už používá uživatel.")
	}
	if err := m.run(ctx, "Skupinu se nepodařilo vytvořit.", "groupadd", name); err != nil {
		return UserList{}, err
	}
	slog.Info("helper: group.create", "group", name, "user", actor)
	return m.list(ctx)
}

func (m *UserManager) editableGroup(db accounts.DB, name string) error {
	if !ValidName(name) || !db.IsEditableGroup(name) {
		return &Error{Code: CodeNotFound, Message: "Skupina neexistuje nebo ji nelze spravovat."}
	}
	return nil
}

func (m *UserManager) GroupSetMembers(ctx context.Context, name string, members []string, actor string) (UserList, error) {
	members, err := validNameList("members", members)
	if err != nil {
		return UserList{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.load()
	if err != nil {
		return UserList{}, err
	}
	if err := m.editableGroup(db, name); err != nil {
		return UserList{}, err
	}
	for _, u := range members {
		if !db.IsNASUser(u) {
			return UserList{}, invalid("members", "Uživatele "+quoteShort(u)+" nelze přidat.")
		}
	}
	// Keep members the UI does not manage (accounts outside nas-users).
	g, _ := db.Group(name)
	all := append([]string{}, members...)
	for _, u := range g.Members {
		if !db.IsNASUser(u) {
			all = append(all, u)
		}
	}
	if err := m.run(ctx, "Členy skupiny se nepodařilo uložit.", "gpasswd", "--members", strings.Join(all, ","), name); err != nil {
		return UserList{}, err
	}
	slog.Info("helper: group.setMembers", "group", name, "members", members, "user", actor)
	return m.list(ctx)
}

func (m *UserManager) GroupDelete(ctx context.Context, name, actor string) (UserList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	db, err := m.load()
	if err != nil {
		return UserList{}, err
	}
	if err := m.editableGroup(db, name); err != nil {
		return UserList{}, err
	}
	if err := m.run(ctx, "Skupinu se nepodařilo smazat.", "groupdel", name); err != nil {
		return UserList{}, err
	}
	slog.Info("helper: group.delete", "group", name, "user", actor)
	return m.list(ctx)
}
