package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nas-revival/backend/internal/accounts"
)

const (
	basePasswd = "root:x:0:0:root:/root:/bin/bash\n" +
		"nas-revival:x:998:998::/nonexistent:/usr/sbin/nologin\n" +
		"debian:x:1000:1000:Debian:/home/debian:/bin/bash\n" +
		"jana:x:1001:990:Jana Nováková:/nonexistent:/usr/sbin/nologin\n"
	baseGroup = "root:x:0:\nsudo:x:27:debian,jana\nnas-users:x:990:\ndebian:x:1000:\n" +
		"rodina:x:1002:jana,debian\nnas-admin:x:991:\n"
	baseShadow = "root:*:20000:0:99999:7:::\nnas-revival:!:20000::::::\ndebian:$y$old:20000:0:99999:7:::\n" +
		"jana:$y$jana:20000:0:99999:7:::\n"
	testPassword = "correct horse battery"
)

// fakeShadow records the commands and applies a simplified version of their
// effect to the account files, so List after a change sees the result.
type fakeShadow struct {
	t      *testing.T
	p      accounts.Paths
	calls  []string
	stdins []string
	fail   map[string]bool // command name → fail
}

func (f *fakeShadow) read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func (f *fakeShadow) write(path, s string) {
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// editShadow replaces field i of the user's shadow line.
func (f *fakeShadow) editShadow(name string, i int, v func(string) string) {
	ls := strings.Split(f.read(f.p.Shadow), "\n")
	for n, l := range ls {
		fs := strings.Split(l, ":")
		if fs[0] == name && len(fs) > i {
			fs[i] = v(fs[i])
			ls[n] = strings.Join(fs, ":")
		}
	}
	f.write(f.p.Shadow, strings.Join(ls, "\n"))
}

func (f *fakeShadow) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.fail[name] {
		return []byte("boom"), errors.New("exit 1")
	}
	last := ""
	if len(args) > 0 {
		last = args[len(args)-1]
	}
	switch name {
	case "groupadd":
		gid := "1003"
		if args[0] == "--system" {
			gid = "989"
		}
		f.write(f.p.Group, f.read(f.p.Group)+last+":x:"+gid+":\n")
	case "useradd":
		f.write(f.p.Passwd, f.read(f.p.Passwd)+last+":x:1004:990::/nonexistent:/usr/sbin/nologin\n")
		f.write(f.p.Shadow, f.read(f.p.Shadow)+last+":!:20000:0:99999:7:::\n")
	case "userdel":
		var keep []string
		for _, l := range strings.Split(f.read(f.p.Passwd), "\n") {
			if !strings.HasPrefix(l, last+":") {
				keep = append(keep, l)
			}
		}
		f.write(f.p.Passwd, strings.Join(keep, "\n"))
	case "usermod":
		for i, a := range args {
			switch a {
			case "--lock":
				f.editShadow(last, 1, func(h string) string { return "!" + h })
			case "--unlock":
				f.editShadow(last, 1, func(h string) string { return strings.TrimPrefix(h, "!") })
			case "--expiredate":
				f.editShadow(last, 7, func(string) string { return args[i+1] })
			}
		}
	}
	return nil, nil
}

func (f *fakeShadow) runStdin(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	f.stdins = append(f.stdins, name+"<"+string(stdin))
	if _, err := f.run(ctx, name, args...); err != nil {
		return []byte("boom"), err
	}
	if name == "chpasswd" {
		user, pw, _ := strings.Cut(strings.TrimSuffix(string(stdin), "\n"), ":")
		h := "$y$" + pw
		if len(args) > 0 && args[0] == "--encrypted" {
			h = pw
		}
		f.editShadow(user, 1, func(string) string { return h })
	}
	return nil, nil
}

func newUsers(t *testing.T, samba bool, smbUsers string) (*UserManager, *fakeShadow) {
	t.Helper()
	dir := t.TempDir()
	p := accounts.Paths{Passwd: filepath.Join(dir, "passwd"), Group: filepath.Join(dir, "group"), Shadow: filepath.Join(dir, "shadow")}
	f := &fakeShadow{t: t, p: p, fail: map[string]bool{}}
	f.write(p.Passwd, basePasswd)
	f.write(p.Group, baseGroup)
	f.write(p.Shadow, baseShadow)
	m := &UserManager{Paths: p, Web: accounts.WebPasswords{Path: filepath.Join(dir, "credentials.json"), OwnerUID: -1, OwnerGID: -1},
		RunStdin: f.runStdin, Samba: func() bool { return samba },
		Hash: func(pw string) (string, error) { return "argon:" + pw, nil },
		Now:  func() time.Time { return time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC) }}
	m.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "pdbedit" {
			f.calls = append(f.calls, "pdbedit -L")
			return []byte(smbUsers), nil
		}
		return f.run(ctx, name, args...)
	}
	return m, f
}

func codeOf(err error) string {
	if e, ok := AsError(err); ok {
		return e.Code
	}
	return ""
}

func TestValidation(t *testing.T) {
	for _, n := range []string{"jana", "a", "petr_2", "x-y"} {
		if !ValidName(n) {
			t.Errorf("%q rejected", n)
		}
	}
	for _, n := range []string{"", "Jana", "2x", "-x", "jána", "a b", "a:b", strings.Repeat("a", 33), "../x"} {
		if ValidName(n) {
			t.Errorf("%q accepted", n)
		}
	}
	if ValidatePassword("") == nil || ValidatePassword("x") != nil || ValidatePassword("long enough\nroot:x") == nil ||
		ValidatePassword(strings.Repeat("a", 257)) == nil || ValidatePassword(testPassword) != nil {
		t.Error("password rules")
	}
	if _, err := ValidateFullName("Jana: admin"); err == nil {
		t.Error("colon in full name accepted")
	}
	if _, err := ValidateUserCreate(UserCreate{Name: "nas-admin", Password: testPassword}); err == nil {
		t.Error("reserved name accepted")
	}
}

func TestListShowsOnlyNASUsersAndEditableGroups(t *testing.T) {
	m, _ := newUsers(t, false, "")
	l, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Users) != 1 || l.Users[0].Name != "jana" || l.Users[0].FullName != "Jana Nováková" {
		t.Fatalf("users = %+v", l.Users)
	}
	u := l.Users[0]
	if strings.Join(u.Groups, ",") != "rodina" || !u.Password.Linux || u.Password.Web || u.Password.Samba != SambaUnavailable {
		t.Errorf("jana = %+v", u)
	}
	var names []string
	for _, g := range l.Groups {
		names = append(names, g.Name)
	}
	// sudo, root, nas-admin and debian's private group are not listed.
	if strings.Join(names, ",") != "nas-users,rodina" {
		t.Errorf("groups = %v", names)
	}
	if g := l.Groups[1]; strings.Join(g.Members, ",") != "jana" || g.Builtin {
		t.Errorf("rodina = %+v (debian is not a NAS user)", g)
	}
	if !l.Groups[0].Builtin || strings.Join(l.Groups[0].Members, ",") != "jana" {
		t.Errorf("nas-users = %+v", l.Groups[0])
	}
}

func TestCreateWritesAllHashesPasswordOnlyOnStdin(t *testing.T) {
	m, f := newUsers(t, true, "jana:1001:\n")
	l, err := m.Create(context.Background(), UserCreate{Name: "petr", FullName: " Petr ", Password: testPassword, Groups: []string{"rodina"}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, testPassword) {
			t.Fatalf("password in argv: %q", c)
		}
	}
	want := "useradd --no-create-home --no-user-group --gid nas-users --home-dir /nonexistent --shell /usr/sbin/nologin --comment Petr --groups rodina petr"
	if f.calls[0] != want {
		t.Errorf("useradd = %q", f.calls[0])
	}
	if len(f.stdins) != 2 || f.stdins[0] != "chpasswd<petr:"+testPassword+"\n" ||
		f.stdins[1] != "smbpasswd<"+testPassword+"\n"+testPassword+"\n" {
		t.Errorf("stdin = %q", f.stdins)
	}
	web, _ := m.Web.Load()
	if web["petr"].Hash != "argon:"+testPassword || web["petr"].Shadow == "" {
		t.Errorf("web record = %+v", web["petr"])
	}
	st, _ := os.Stat(m.Web.Path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("credentials mode %v", st.Mode().Perm())
	}
	var found bool
	for _, u := range l.Users {
		if u.Name == "petr" {
			found = true
			// useradd leaves "!" in shadow; that is "no password yet", not a disabled account.
			if !u.Password.Web || u.Password.ChangedOutside || u.Disabled {
				t.Errorf("petr = %+v", u)
			}
		}
	}
	if !found {
		t.Error("petr not listed")
	}
}

func TestCreateCreatesUsersGroupOnFirstUse(t *testing.T) {
	m, f := newUsers(t, false, "")
	f.write(f.p.Group, "root:x:0:\nrodina:x:1002:\n")
	if _, err := m.Create(context.Background(), UserCreate{Name: "petr", Password: testPassword}, "admin"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "groupadd --system nas-users" {
		t.Errorf("calls = %q", f.calls)
	}
}

func TestCreateRejectsSystemGroupsAndExistingNames(t *testing.T) {
	m, f := newUsers(t, false, "")
	for _, c := range []UserCreate{
		{Name: "petr", Password: testPassword, Groups: []string{"sudo"}},
		{Name: "petr", Password: testPassword, Groups: []string{"nas-admin"}},
		{Name: "debian", Password: testPassword},
		{Name: "rodina", Password: testPassword},
	} {
		if _, err := m.Create(context.Background(), c, "admin"); codeOf(err) != CodeInvalid {
			t.Errorf("%+v: err = %v", c, err)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("commands ran: %q", f.calls)
	}
}

func TestCreateRollsBackWhenPasswordFails(t *testing.T) {
	m, f := newUsers(t, false, "")
	f.fail["chpasswd"] = true
	if _, err := m.Create(context.Background(), UserCreate{Name: "petr", Password: testPassword}, "admin"); err == nil {
		t.Fatal("no error")
	}
	if f.calls[len(f.calls)-1] != "userdel petr" {
		t.Errorf("calls = %q", f.calls)
	}
}

func TestSambaFailureRestoresShadowHash(t *testing.T) {
	m, f := newUsers(t, true, "jana:1001:\n")
	f.fail["smbpasswd"] = true
	_, err := m.SetPassword(context.Background(), "jana", testPassword, "admin")
	if err == nil || !strings.Contains(err.Error(), "zůstalo původní") {
		t.Fatalf("err = %v", err)
	}
	if last := f.stdins[len(f.stdins)-1]; last != "chpasswd<jana:$y$jana\n" {
		t.Errorf("restore = %q", last)
	}
	if db, _ := accounts.Load(m.Paths); db.Shadow["jana"].Hash != "$y$jana" {
		t.Errorf("shadow = %q", db.Shadow["jana"].Hash)
	}
	if web, _ := m.Web.Load(); len(web) != 0 {
		t.Error("web hash written despite failure")
	}
}

func TestPasswordChangedOutsideAndDisabledStaysLocked(t *testing.T) {
	m, f := newUsers(t, false, "")
	ctx := context.Background()
	if _, err := m.SetDisabled(ctx, "jana", true, "admin"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.calls, "|"), "usermod --lock --expiredate 1 jana") {
		t.Errorf("calls = %q", f.calls)
	}
	if _, err := m.SetPassword(ctx, "jana", testPassword, "admin"); err != nil {
		t.Fatal(err)
	}
	l, _ := m.List(ctx)
	if u := l.Users[0]; !u.Disabled || u.Password.ChangedOutside || !u.Password.Web {
		t.Fatalf("after password on disabled account: %+v", u)
	}
	f.editShadow("jana", 1, func(string) string { return "!$y$console" })
	l, _ = m.List(ctx)
	if !l.Users[0].Password.ChangedOutside {
		t.Error("outside change not detected")
	}
	if _, err := m.SetDisabled(ctx, "jana", false, "admin"); err != nil {
		t.Fatal(err)
	}
	l, _ = m.List(ctx)
	if l.Users[0].Disabled {
		t.Errorf("still disabled: %q", f.calls)
	}
	web, _ := m.Web.Load()
	if web["jana"].Disabled {
		t.Error("web flag still disabled")
	}
}

func TestSystemAccountsAreNotManaged(t *testing.T) {
	m, f := newUsers(t, false, "")
	ctx := context.Background()
	for _, n := range []string{"root", "nas-revival", "debian", "nobody", "../etc"} {
		if _, err := m.Delete(ctx, n, "admin"); codeOf(err) != CodeNotFound {
			t.Errorf("delete %s: %v", n, err)
		}
		if _, err := m.SetPassword(ctx, n, testPassword, "admin"); codeOf(err) != CodeNotFound {
			t.Errorf("password %s: %v", n, err)
		}
	}
	for _, g := range []string{"sudo", "nas-users", "debian", "root"} {
		if _, err := m.GroupDelete(ctx, g, "admin"); codeOf(err) != CodeNotFound {
			t.Errorf("group delete %s: %v", g, err)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("commands ran: %q", f.calls)
	}
}

func TestUpdateKeepsUnmanagedGroups(t *testing.T) {
	m, f := newUsers(t, false, "")
	if _, err := m.Update(context.Background(), UserUpdate{Name: "jana", FullName: "Jana N", Groups: nil}, "admin"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "usermod --comment Jana N --groups sudo jana" {
		t.Errorf("usermod = %q", f.calls[0])
	}
}

func TestGroupMembersKeepNonNASMembers(t *testing.T) {
	m, f := newUsers(t, false, "")
	ctx := context.Background()
	if _, err := m.GroupSetMembers(ctx, "rodina", []string{"debian"}, "admin"); codeOf(err) != CodeInvalid {
		t.Errorf("non-NAS member: %v", err)
	}
	if _, err := m.GroupSetMembers(ctx, "rodina", []string{}, "admin"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "gpasswd --members debian rodina" {
		t.Errorf("gpasswd = %q", f.calls[0])
	}
}

func TestDeleteRemovesSambaAndWebRecord(t *testing.T) {
	m, f := newUsers(t, true, "jana:1001:\n")
	ctx := context.Background()
	if _, err := m.SetPassword(ctx, "jana", testPassword, "admin"); err != nil {
		t.Fatal(err)
	}
	l, err := m.Delete(ctx, "jana", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.calls, "|"), "smbpasswd -x jana|userdel jana") {
		t.Errorf("calls = %q", f.calls)
	}
	if web, _ := m.Web.Load(); len(web) != 0 || len(l.Users) != 0 {
		t.Errorf("left behind: web=%v users=%v", web, l.Users)
	}
}

func TestDispatcherRoutesUserOps(t *testing.T) {
	m, _ := newUsers(t, false, "")
	d := &Dispatcher{Users: m}
	res, err := d.Handle(context.Background(), request{Op: "user.list"})
	if err != nil || len(res.(UserList).Users) != 1 {
		t.Fatalf("list: %v %v", res, err)
	}
	if _, err := d.Handle(context.Background(), request{Op: "user.list", Args: []byte(`{"bogus":1}`)}); codeOf(err) != CodeInvalid {
		t.Errorf("unknown field: %v", err)
	}
	if _, err := (&Dispatcher{}).Handle(context.Background(), request{Op: "user.list"}); codeOf(err) != CodeInvalid {
		t.Errorf("disabled manager: %v", err)
	}
}

// A deleted account takes its SSH keys along; a new account with the same
// name starts without the keys of an old one.
func TestUserKeysRemovedOnDeleteAndCreate(t *testing.T) {
	m, _ := newUsers(t, false, "")
	m.SshKeysDir = t.TempDir()
	ctx := context.Background()
	keys := filepath.Join(m.SshKeysDir, "jana")
	os.WriteFile(keys, []byte("ssh-ed25519 AAAA old\n"), 0o644)
	if _, err := m.Delete(ctx, "jana", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keys); !os.IsNotExist(err) {
		t.Fatalf("keys kept after delete: %v", err)
	}
	os.WriteFile(filepath.Join(m.SshKeysDir, "petr"), []byte("ssh-ed25519 AAAA stale\n"), 0o644)
	if _, err := m.Create(ctx, UserCreate{Name: "petr", Password: testPassword, Role: accounts.RoleUser}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.SshKeysDir, "petr")); !os.IsNotExist(err) {
		t.Fatalf("stale keys inherited: %v", err)
	}
}
