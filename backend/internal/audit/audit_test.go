package audit

import (
	"bytes"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func listen(t *testing.T) (string, *net.UnixConn) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "journal.sock")
	c, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: p, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return p, c
}

func recv(t *testing.T, c *net.UnixConn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8192)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf[:n])
}

func TestLogSendsNativeProtocol(t *testing.T) {
	p, c := listen(t)
	(&Logger{Socket: p}).Log(Entry{Action: "user.create", User: "alice", Client: "10.0.0.5", Target: "bob",
		Result: OK, Message: "Účet bob vytvořen"})
	got := recv(t, c)
	for _, want := range []string{"MESSAGE=Účet bob vytvořen\n", "PRIORITY=6\n", "SYSLOG_IDENTIFIER=nas-revival\n",
		"NAS_REVIVAL_AUDIT=1\n", "AUDIT_ACTION=user.create\n", "AUDIT_USER=alice\n", "AUDIT_CLIENT=10.0.0.5\n",
		"AUDIT_TARGET=bob\n", "AUDIT_RESULT=ok\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestEmptyFieldsOmittedAndPriority(t *testing.T) {
	p, c := listen(t)
	(&Logger{Socket: p}).Log(Entry{Action: "login.fail", Result: Fail, Priority: 4})
	got := recv(t, c)
	for _, no := range []string{"AUDIT_USER", "AUDIT_CLIENT", "AUDIT_TARGET"} {
		if strings.Contains(got, no) {
			t.Errorf("%s should be omitted: %q", no, got)
		}
	}
	if !strings.Contains(got, "PRIORITY=4\n") || !strings.Contains(got, "MESSAGE=login.fail\n") {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeAndInjection(t *testing.T) {
	p, c := listen(t)
	(&Logger{Socket: p}).Log(Entry{Action: "x", User: "a\nNAS_REVIVAL_AUDIT=0\x00b", Target: "\xff" + strings.Repeat("é", 500)})
	got := recv(t, c)
	if strings.Count(got, "\n") != 7 || strings.Contains(got, "\x00") {
		t.Errorf("control characters survived: %q", got)
	}
	if !strings.Contains(got, "AUDIT_USER=a NAS_REVIVAL_AUDIT=0 b\n") {
		t.Errorf("got %q", got)
	}
	if n := len([]rune(Sanitize(strings.Repeat("é", 500)))); n != MaxValue+1 {
		t.Errorf("cap = %d", n)
	}
}

func TestEncodeBinaryForNewline(t *testing.T) {
	got := Encode([]string{"A", "B"}, []string{"x\ny", "z"})
	want := "A\n\x03\x00\x00\x00\x00\x00\x00\x00x\ny\nB=z\n"
	if string(got) != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFallbackToSlog(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)
	(&Logger{Socket: filepath.Join(t.TempDir(), "none")}).Log(Entry{Action: "login", User: "alice", Client: "10.0.0.5", Result: OK, Message: "Přihlášení"})
	out := buf.String()
	if !strings.Contains(out, "audit_action=login") || !strings.Contains(out, "audit_user=alice") || !strings.Contains(out, "level=INFO") {
		t.Errorf("slog = %q", out)
	}
	buf.Reset()
	(&Logger{Socket: filepath.Join(t.TempDir(), "none")}).Log(Entry{Action: "login.fail", Priority: 4})
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("slog = %q", buf.String())
	}
}
