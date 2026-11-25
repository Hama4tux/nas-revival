package helper

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startServer(t *testing.T, uid uint32) (*Client, *fixture) {
	t.Helper()
	f := newFixture(t)
	sock := filepath.Join(t.TempDir(), "h.sock")
	ln, err := Listen(sock, -1)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(sock)
	if st.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode %v", st.Mode())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	tm := &TimeManager{Dropin: filepath.Join(t.TempDir(), "d", "x.conf"), Run: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }}
	go func() { Serve(ctx, ln, uid, &Dispatcher{Net: f.m, Time: tm}); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return &Client{Socket: sock, Timeout: 3 * time.Second}, f
}

func TestServerRoundTrip(t *testing.T) {
	c, f := startServer(t, uint32(os.Getuid()))
	ctx := context.Background()
	s, err := c.NetworkGet(ctx)
	if err != nil || s.Interface != "enp1s0" {
		t.Fatalf("%+v %v", s, err)
	}
	s, err = c.NetworkApply(ctx, newCfg)
	if err != nil || s.Pending == nil {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := c.NetworkApply(ctx, newCfg); err == nil || err.(*Error).Code != CodeChangePending {
		t.Fatal(err)
	}
	bad := newCfg
	bad.PrefixLength = ptr(5)
	f.m.Confirm(ctx)
	if _, err := c.NetworkApply(ctx, bad); err == nil || err.(*Error).Field != "prefixLength" {
		t.Fatalf("%v", err)
	}
	if _, err := c.NetworkConfirm(ctx); err == nil || err.(*Error).Code != CodeNothingPending {
		t.Fatal(err)
	}
	if ts, err := c.TimeApply(ctx, []string{"tik.cesnet.cz"}); err != nil || ts.NTPServers[0] != "tik.cesnet.cz" {
		t.Fatalf("%+v %v", ts, err)
	}
	if err := c.Call(ctx, "rm.rf", nil, nil); err == nil || err.(*Error).Code != CodeInvalid {
		t.Fatal(err)
	}
}

func TestServerRejectsOtherUID(t *testing.T) {
	c, _ := startServer(t, uint32(os.Getuid())+1)
	_, err := c.NetworkGet(context.Background())
	if e, ok := AsError(err); !ok || e.Code != CodeHelperUnavailable {
		t.Fatalf("%v", err)
	}
}

func TestServerLimitsAndUnknownFields(t *testing.T) {
	c, _ := startServer(t, uint32(os.Getuid()))
	raw := func(payload string) response {
		conn, err := net.Dial("unix", c.Socket)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.Write([]byte(payload))
		var r response
		json.NewDecoder(conn).Decode(&r)
		return r
	}
	if r := raw(`{"op":"network.get","args":{"pad":"` + strings.Repeat("a", MaxRequest) + `"}}`); r.OK || r.Error == nil {
		t.Fatalf("oversize accepted: %+v", r)
	}
	if r := raw(`{"op":"network.apply","args":{"mode":"dhcp","evil":1}}`); r.OK || r.Error.Code != CodeInvalid {
		t.Fatalf("%+v", r)
	}
	if r := raw(`not json`); r.OK {
		t.Fatal("garbage accepted")
	}
}

func TestClientUnavailable(t *testing.T) {
	c := &Client{Socket: filepath.Join(t.TempDir(), "none")}
	if _, err := c.TimeGet(context.Background()); err == nil || err.(*Error).Code != CodeHelperUnavailable {
		t.Fatal(err)
	}
}
