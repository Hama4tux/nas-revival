package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"nas-revival/backend/internal/auth"
	"nas-revival/backend/internal/diskactivity"
	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/helper"
)

type fakeActivity struct {
	seconds []int
	err     error
}

func (f *fakeActivity) DiskActivity(_ context.Context, seconds int) (diskactivity.Result, error) {
	f.seconds = append(f.seconds, seconds)
	if f.err != nil {
		return diskactivity.Result{}, f.err
	}
	return diskactivity.Result{Seconds: seconds, Devices: []diskactivity.DeviceActivity{
		{Name: "sda", DevStatDelta: diskactivity.DevStatDelta{WriteOps: 4, WriteBytes: 16384}},
		{Name: "sda1", Parent: "sda", Mount: "/", DevStatDelta: diskactivity.DevStatDelta{WriteOps: 4}},
		{Name: "sdb"},
	}, Files: []diskactivity.FileActivity{{Path: "/var/log/journal/a/system.journal", Device: "sda1"}}}, nil
}

func newActivityServer(t *testing.T, a *fakeActivity) (http.Handler, *http.Cookie) {
	t.Helper()
	h, _ := auth.HashPassword("secret")
	v, _ := auth.NewVerifier(auth.Credentials{Username: "admin", Hash: h})
	p := &fakePower{cfg: diskpower.Config{Disks: map[string]diskpower.Settings{}}}
	s := &Server{Verifier: v, Sessions: auth.NewSessions(), Limiter: auth.NewLimiter(),
		Overview: fakeOverview{}, Smart: fakeSmart{}, Storage: fakeStorage{}, DiskPower: p}
	if a != nil {
		s.DiskActivity = a
	}
	hd := s.Handler()
	return hd, sessionCookie(t, login(t, hd, "secret"))
}

func TestDiskActivity(t *testing.T) {
	a := &fakeActivity{}
	h, c := newActivityServer(t, a)
	if rec := do(h, "GET", "/api/v1/storage/activity", "", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without session = %d", rec.Code)
	}
	rec := do(h, "GET", "/api/v1/storage/activity", "", nil, c)
	var v diskActivityView
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(a.seconds) != 1 || a.seconds[0] != defaultActivitySeconds || len(v.Disks) != 2 {
		t.Fatalf("seconds=%v view=%+v", a.seconds, v)
	}
	sda := v.Disks[0]
	// Default balanced profile on the system disk has no standby timeout.
	if sda.ID != "wwn-0x1" || !sda.System || len(sda.Partitions) != 1 ||
		len(sda.Hints) != 2 || sda.Hints[0].Code != diskactivity.HintNoStandby || sda.Hints[1].Code != diskactivity.HintJournald {
		t.Fatalf("sda = %+v", sda)
	}
	if v.Disks[1].ID != "usb-stick" || v.Disks[1].Hints[0].Code != diskactivity.HintIdle {
		t.Fatalf("sdb = %+v", v.Disks[1])
	}
	if rec := do(h, "GET", "/api/v1/storage/activity?seconds=30", "", nil, c); rec.Code != 200 || a.seconds[1] != 30 {
		t.Fatalf("seconds=30: %d %v", rec.Code, a.seconds)
	}
	for _, q := range []string{"5", "46", "x"} {
		if rec := do(h, "GET", "/api/v1/storage/activity?seconds="+q, "", nil, c); rec.Code != 400 {
			t.Errorf("seconds=%s: %d", q, rec.Code)
		}
	}
	if len(a.seconds) != 2 {
		t.Fatalf("invalid requests reached the helper: %v", a.seconds)
	}
}

func TestDiskActivityErrors(t *testing.T) {
	h, c := newActivityServer(t, nil)
	if rec := do(h, "GET", "/api/v1/storage/activity", "", nil, c); rec.Code != 503 {
		t.Fatalf("no helper = %d", rec.Code)
	}
	a := &fakeActivity{err: &helper.Error{Code: helper.CodeActivityRunning, Message: "běží"}}
	h, c = newActivityServer(t, a)
	if rec := do(h, "GET", "/api/v1/storage/activity", "", nil, c); rec.Code != 409 ||
		!strings.Contains(rec.Body.String(), helper.CodeActivityRunning) {
		t.Fatalf("running = %d %s", rec.Code, rec.Body)
	}
}
