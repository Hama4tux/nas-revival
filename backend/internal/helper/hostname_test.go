package helper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHostname(t *testing.T) {
	ok := map[string]string{"nas": "nas", " NSA310 ": "nsa310", "nas-1": "nas-1", strings.Repeat("a", 63): strings.Repeat("a", 63)}
	for in, want := range ok {
		if got, err := ValidateHostname(in); err != nil || got != want {
			t.Errorf("%q → %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "-nas", "nas-", "nas.home", "nas_1", "náš", "123", "localhost", strings.Repeat("a", 64), "a b", "--help"} {
		if _, err := ValidateHostname(in); codeOf(err) != CodeInvalid {
			t.Errorf("%q accepted (%v)", in, err)
		}
	}
}

func TestRenderHosts(t *testing.T) {
	base := "127.0.0.1\tlocalhost\n127.0.1.1\tnsa310\n::1\tlocalhost ip6-localhost\n"
	if got := RenderHosts(base, "nas"); got != "127.0.0.1\tlocalhost\n127.0.1.1\tnas\n::1\tlocalhost ip6-localhost\n" {
		t.Errorf("replace: %q", got)
	}
	if got := RenderHosts("127.0.0.1 localhost", "nas"); got != "127.0.0.1 localhost\n127.0.1.1\tnas\n" {
		t.Errorf("append: %q", got)
	}
}

func TestHostnameSet(t *testing.T) {
	hosts := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(hosts, []byte("127.0.0.1 localhost\n127.0.1.1 nsa310\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	m := &HostnameManager{HostsFile: hosts, Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}}
	st, err := m.Set(context.Background(), "Nas", "admin")
	if err != nil || st.Hostname != "nas" {
		t.Fatalf("%+v, %v", st, err)
	}
	if len(calls) != 1 || calls[0] != "hostnamectl --static --transient set-hostname nas" {
		t.Errorf("calls = %q", calls)
	}
	if b, _ := os.ReadFile(hosts); string(b) != "127.0.0.1 localhost\n127.0.1.1\tnas\n" {
		t.Errorf("hosts = %q", b)
	}

	calls = nil
	if _, err := m.Set(context.Background(), "-x", "admin"); codeOf(err) != CodeInvalid || len(calls) != 0 {
		t.Errorf("invalid name ran %q, %v", calls, err)
	}
	m.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("exit 1") }
	if _, err := m.Set(context.Background(), "other", "admin"); codeOf(err) != CodeInternal {
		t.Errorf("hostnamectl failure: %v", err)
	}
	if b, _ := os.ReadFile(hosts); !strings.Contains(string(b), "\tnas\n") {
		t.Errorf("hosts changed after a failure: %q", b)
	}
}
