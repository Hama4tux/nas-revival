package helper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func writeStat(t *testing.T, root string, pid int, name string, ppid int, start uint64) {
	t.Helper()
	f := make([]string, 50)
	for i := range f {
		f[i] = "0"
	}
	f[0], f[1], f[19] = "S", fmt.Sprint(ppid), fmt.Sprint(start)
	d := filepath.Join(root, "proc", fmt.Sprint(pid))
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "stat"), []byte(fmt.Sprintf("%d (%s) %s\n", pid, name, strings.Join(f, " "))), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProcessStop(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 1, "systemd", 0, 1)
	writeStat(t, root, 40, "kworker/0:2", 2, 5)
	writeStat(t, root, 300, "sshd", 1, 50)
	writeStat(t, root, 301, "nas-revival", 1, 60)
	writeStat(t, root, 777, "helper-self", 1, 70)
	writeStat(t, root, 900, "rsync", 1, 1234)
	type sent struct {
		pid int
		sig syscall.Signal
	}
	var got []sent
	m := &ProcessManager{Root: root, Self: 777, Kill: func(pid int, sig syscall.Signal) error {
		got = append(got, sent{pid, sig})
		return nil
	}}
	ctx := context.Background()

	r, err := m.Stop(ctx, 900, 1234, false, "admin")
	if err != nil || r.Signal != "TERM" || r.Name != "rsync" {
		t.Fatalf("term: %+v %v", r, err)
	}
	if _, err := m.Stop(ctx, 900, 1234, true, "admin"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (sent{900, syscall.SIGTERM}) || got[1] != (sent{900, syscall.SIGKILL}) {
		t.Fatalf("signals %+v", got)
	}

	got = nil
	refused := map[string]struct {
		pid   int
		start uint64
		code  string
	}{
		"pid 1":           {1, 1, CodeProcessProtected},
		"kernel":          {40, 5, CodeProcessProtected},
		"sshd":            {300, 50, CodeProcessProtected},
		"web/helper":      {301, 60, CodeProcessProtected},
		"self":            {777, 70, CodeProcessProtected},
		"reused pid":      {900, 999, CodeProcessChanged},
		"gone":            {901, 1, CodeNotFound},
		"zero pid":        {0, 1, CodeInvalid},
		"negative":        {-5, 1, CodeInvalid},
		"start 0 differs": {900, 0, CodeProcessChanged},
		"huge pid":        {1 << 30, 1, CodeInvalid},
	}
	for name, c := range refused {
		if _, err := m.Stop(ctx, c.pid, c.start, true, "admin"); codeOf(err) != c.code {
			t.Errorf("%s: got %v, want %s", name, err, c.code)
		}
	}
	if len(got) != 0 {
		t.Fatalf("refused requests sent signals: %+v", got)
	}
}

func TestProcessKillDispatch(t *testing.T) {
	root := t.TempDir()
	writeStat(t, root, 900, "rsync", 1, 1234)
	var pid int
	d := &Dispatcher{Procs: &ProcessManager{Root: root, Self: 1, Kill: func(p int, _ syscall.Signal) error { pid = p; return nil }}}
	if _, err := d.Handle(context.Background(), request{Op: "process.kill", Args: []byte(`{"pid":900,"start":1234}`), User: "admin"}); err != nil || pid != 900 {
		t.Fatalf("dispatch: pid %d err %v", pid, err)
	}
	if _, err := d.Handle(context.Background(), request{Op: "process.kill", Args: []byte(`{"pid":900,"start":1234,"signal":9}`)}); codeOf(err) != CodeInvalid {
		t.Fatalf("unknown field: %v", err)
	}
}
