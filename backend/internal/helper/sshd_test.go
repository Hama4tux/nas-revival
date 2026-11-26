package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/sshd"
)

type fakeSshd struct {
	rejectTest bool
	failReload bool
	listen     string // ss output
	seenDropIn string // drop-in content when sshd -t ran
	dropIn     string
	calls      []string
}

func (f *fakeSshd) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch call {
	case "sshd -T":
		return []byte("port 22\npasswordauthentication yes\npermitrootlogin without-password\n"), nil
	case "sshd -t":
		b, _ := os.ReadFile(f.dropIn)
		f.seenDropIn = string(b)
		if f.rejectTest {
			return []byte("bad option"), errors.New("exit 255")
		}
	case "ss -H -l -t -n":
		return []byte(f.listen), nil
	case "systemctl reload ssh.service":
		if f.failReload {
			return nil, errors.New("exit 1")
		}
	}
	return nil, nil
}

func newSshFixture(t *testing.T) (*SshManager, *fakeSshd) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "passwd"), []byte(basePasswd), 0o644)
	os.WriteFile(filepath.Join(dir, "group"), []byte(baseGroup), 0o644)
	f := &fakeSshd{dropIn: filepath.Join(dir, "10-nas-revival.conf"),
		listen: "tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:*\ntcp LISTEN 0 4096 *:8443 *:*\ntcp LISTEN 0 50 0.0.0.0:445 0.0.0.0:*\n"}
	// "/" passes sshd's chroot rule (root-owned, 0755).
	m := &SshManager{Path: filepath.Join(dir, "ssh.json"), DropIn: f.dropIn, KeysDir: filepath.Join(dir, "keys"),
		Chroot: "/", Accounts: accounts.Paths{Passwd: filepath.Join(dir, "passwd"), Group: filepath.Join(dir, "group")},
		Run: f.run}
	return m, f
}

func TestSshGetUnconfigured(t *testing.T) {
	m, _ := newSshFixture(t)
	st, err := m.Get(context.Background())
	if err != nil || st.Managed || !st.Running || !reflect.DeepEqual(st.Settings, sshd.Default()) {
		t.Fatalf("get = %+v, %v", st, err)
	}
	if !reflect.DeepEqual(st.Effective.Ports, []int{22}) || !st.Effective.PasswordAuth {
		t.Fatalf("effective = %+v", st.Effective)
	}
	// nas-users and editable groups only: no root, sudo or private groups.
	if !reflect.DeepEqual(st.Groups, []string{"nas-users", "rodina"}) || !st.ChrootOK {
		t.Fatalf("groups = %v, chroot %v", st.Groups, st.ChrootOK)
	}
}

func TestSshSetWritesTestsAndReloads(t *testing.T) {
	m, f := newSshFixture(t)
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	in := sshd.Settings{Port: 2222, PasswordAuth: false, SftpGroups: []string{"rodina"}}
	st, err := m.Set(context.Background(), in, "jana")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Managed || st.Settings.Port != 2222 {
		t.Fatalf("state = %+v", st)
	}
	if !strings.Contains(f.seenDropIn, "Port 2222\n") || !strings.Contains(f.seenDropIn, "Match Group rodina\n") {
		t.Fatalf("sshd -t saw:\n%s", f.seenDropIn)
	}
	if !slices.Contains(f.calls, "systemctl reload ssh.service") {
		t.Fatalf("calls = %v", f.calls)
	}
	// sshd reads authorized_keys with the user's uid: the directory must be
	// 0755 even under the helper's UMask=0077.
	if fi, err := os.Stat(m.KeysDir); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o755 {
		t.Fatalf("keys dir: %v %v", fi.Mode(), err)
	}
	if got, _ := sshd.Load(m.Path); got.Port != 2222 {
		t.Fatalf("ssh.json = %+v", got)
	}
}

func TestSshSetRejectedKeepsOldDropIn(t *testing.T) {
	m, f := newSshFixture(t)
	os.WriteFile(m.DropIn, []byte("# old\n"), 0o644)
	f.rejectTest = true
	_, err := m.Set(context.Background(), sshd.Settings{Port: 2222, PasswordAuth: true}, "jana")
	var he *Error
	if !errors.As(err, &he) || he.Code != CodeInternal {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(m.DropIn); string(b) != "# old\n" {
		t.Fatalf("drop-in = %q", b)
	}
	if _, err := os.Stat(m.Path); err == nil {
		t.Fatal("ssh.json written although sshd rejected the config")
	}
	if slices.Contains(f.calls, "systemctl reload ssh.service") {
		t.Fatal("reloaded a rejected config")
	}
}

func TestSshSetRejectedWithoutOldDropInRemovesIt(t *testing.T) {
	m, f := newSshFixture(t)
	f.rejectTest = true
	if _, err := m.Set(context.Background(), sshd.Default(), "jana"); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(m.DropIn); !os.IsNotExist(err) {
		t.Fatalf("drop-in left behind: %v", err)
	}
}

func TestSshSetValidation(t *testing.T) {
	m, f := newSshFixture(t)
	cases := []struct {
		s     sshd.Settings
		field string
	}{
		{sshd.Settings{Port: 445}, "port"},  // below 1024
		{sshd.Settings{Port: 8443}, "port"}, // web UI
		{sshd.Settings{Port: 22, SftpGroups: []string{"sudo"}}, "sftpGroups"},
		{sshd.Settings{Port: 22, SftpGroups: []string{"debian"}}, "sftpGroups"}, // private group
		{sshd.Settings{Port: 22, SftpGroups: []string{"neni"}}, "sftpGroups"},
	}
	for _, c := range cases {
		_, err := m.Set(context.Background(), c.s, "jana")
		var he *Error
		if !errors.As(err, &he) || he.Code != CodeInvalid || he.Field != c.field {
			t.Errorf("%+v: err = %v", c.s, err)
		}
	}
	f.listen += "tcp LISTEN 0 10 0.0.0.0:5000 0.0.0.0:*\n"
	_, err := m.Set(context.Background(), sshd.Settings{Port: 5000}, "jana")
	var he *Error
	if !errors.As(err, &he) || he.Field != "port" {
		t.Fatalf("busy port: %v", err)
	}
	// sshd's own port is not "busy".
	if _, err := m.Set(context.Background(), sshd.Settings{Port: 22}, "jana"); err != nil {
		t.Fatalf("own port: %v", err)
	}
	if _, err := os.Stat(m.DropIn); err != nil {
		t.Fatal(err)
	}
}

func TestSshSftpNeedsSafeChroot(t *testing.T) {
	m, _ := newSshFixture(t)
	m.Chroot = t.TempDir() // owned by the test user, not root
	_, err := m.Set(context.Background(), sshd.Settings{Port: 22, SftpGroups: []string{"nas-users"}}, "jana")
	var he *Error
	if !errors.As(err, &he) || he.Field != "sftpGroups" {
		t.Fatalf("err = %v", err)
	}
	// Without SFTP the chroot does not matter.
	if _, err := m.Set(context.Background(), sshd.Settings{Port: 22}, "jana"); err != nil {
		t.Fatal(err)
	}
}

func TestSshReloadFailureReported(t *testing.T) {
	m, f := newSshFixture(t)
	f.failReload = true
	_, err := m.Set(context.Background(), sshd.Default(), "jana")
	if err == nil || !strings.Contains(err.Error(), "uložené") {
		t.Fatalf("err = %v", err)
	}
}

func TestSshRestore(t *testing.T) {
	m, _ := newSshFixture(t)
	if _, err := m.Set(context.Background(), sshd.Settings{Port: 2222}, "jana"); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Path); !os.IsNotExist(err) {
		t.Fatal("ssh.json kept for a version without it")
	}
	b, _ := os.ReadFile(m.DropIn)
	if string(b) != sshd.Render(sshd.Default()) {
		t.Fatalf("drop-in = %s", b)
	}
	if err := m.Restore(context.Background(), []byte(`{"version":1,"port":2200,"passwordAuth":false,"sftpGroups":[]}`), true); err != nil {
		t.Fatal(err)
	}
	if got, _ := sshd.Load(m.Path); got.Port != 2200 || got.PasswordAuth {
		t.Fatalf("restored %+v", got)
	}
	if err := m.Restore(context.Background(), []byte(`{"version":1,"port":23}`), true); err == nil {
		t.Fatal("invalid version restored")
	}
}

func TestSshViaDispatcherRecordsHistory(t *testing.T) {
	m, _ := newSshFixture(t)
	d := &Dispatcher{Ssh: m}
	if _, err := d.handleSsh(context.Background(), request{Op: "ssh.set", Args: []byte(`{"settings":{"port":2222,"passwordAuth":true,"sftpGroups":[]}}`)}, "jana"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.handleSsh(context.Background(), request{Op: "ssh.set", Args: []byte(`{}`)}, "jana"); err == nil {
		t.Fatal("missing settings accepted")
	}
	if _, err := d.handleSsh(context.Background(), request{Op: "ssh.set", Args: []byte(`{"settings":{"port":22,"permitRootLogin":"yes"}}`)}, "jana"); err == nil {
		t.Fatal("unknown field accepted")
	}
}
