package helper

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateNTP(t *testing.T) {
	good := [][]string{nil, {}, {"tik.cesnet.cz", "192.0.2.1", "2001:db8::1", "0.debian.pool.ntp.org"}, {"a"}}
	for _, g := range good {
		if _, err := ValidateNTP(g); err != nil {
			t.Errorf("%v: %v", g, err)
		}
	}
	long := strings.Repeat("a", 64) + ".cz"
	bad := [][]string{
		{"a", "b", "c", "d", "e"}, {""}, {"has space.cz"}, {"a\nb"}, {"a;b"}, {"$(id)"}, {"a|b"}, {"`x`"},
		{"-a.cz"}, {"a-.cz"}, {"a..cz"}, {"a.cz."}, {long}, {strings.Repeat("a.", 130)}, {"0.0.0.0"}, {"fe80::1%eth0"},
		{"x.cz", "X.cz"}, {"ü.cz"}, {"a b"}, {"a\x00"},
	}
	for _, b := range bad {
		if _, err := ValidateNTP(b); err == nil {
			t.Errorf("accepted %q", b)
		}
	}
}

func TestTimesyncdRender(t *testing.T) {
	if RenderTimesyncd(nil) != "" {
		t.Fatal()
	}
	r := RenderTimesyncd([]string{"a.cz", "10.0.0.1"})
	if !strings.Contains(r, "[Time]\nNTP=a.cz 10.0.0.1\n") {
		t.Fatal(r)
	}
	if got := ParseTimesyncd(r); !reflect.DeepEqual(got, []string{"a.cz", "10.0.0.1"}) {
		t.Fatal(got)
	}
}

func fakeRun(calls *[]string, outs map[string]string) Runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		*calls = append(*calls, line)
		return []byte(outs[line]), nil
	}
}

func TestTimeManager(t *testing.T) {
	drop := filepath.Join(t.TempDir(), "timesyncd.conf.d", "50-nas-revival.conf")
	var calls []string
	outs := map[string]string{
		"timedatectl show -p Timezone -p NTPSynchronized":                                                    "Timezone=Europe/Prague\nNTPSynchronized=yes\n",
		"timedatectl show-timesync -p ServerName -p ServerAddress -p SystemNTPServers -p FallbackNTPServers": "ServerName=0.debian.pool.ntp.org\nServerAddress=192.0.2.1\nSystemNTPServers=\nFallbackNTPServers=0.debian.pool.ntp.org 1.debian.pool.ntp.org\n",
	}
	tm := &TimeManager{Dropin: drop, Run: fakeRun(&calls, outs)}
	st, err := tm.Get(context.Background())
	if err != nil || !st.DefaultServers || !st.Synchronized || st.Timezone != "Europe/Prague" || len(st.NTPServers) != 2 || st.ServerAddress != "192.0.2.1" {
		t.Fatalf("%+v %v", st, err)
	}
	st, err = tm.Apply(context.Background(), []string{"tik.cesnet.cz"})
	if err != nil || st.DefaultServers || !reflect.DeepEqual(st.NTPServers, []string{"tik.cesnet.cz"}) {
		t.Fatalf("%+v %v", st, err)
	}
	if b, _ := os.ReadFile(drop); !strings.Contains(string(b), "NTP=tik.cesnet.cz") {
		t.Fatal(string(b))
	}
	if calls[len(calls)-3] != "systemctl restart systemd-timesyncd" {
		t.Fatal(calls)
	}
	if _, err := tm.Apply(context.Background(), []string{"bad host"}); err == nil {
		t.Fatal("accepted")
	}
	st, err = tm.Apply(context.Background(), nil)
	if err != nil || !st.DefaultServers {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := os.Stat(drop); !os.IsNotExist(err) {
		t.Fatal("drop-in not removed")
	}
	// missing values: all commands return nothing
	tm2 := &TimeManager{Dropin: drop, Run: func(context.Context, string, ...string) ([]byte, error) { return nil, os.ErrNotExist }}
	if st, _ := tm2.Get(context.Background()); st.NTPServers == nil || !st.DefaultServers {
		t.Fatalf("%+v", st)
	}
}
