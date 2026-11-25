package helper

import (
	"context"
	"errors"
	"testing"
	"time"

	"nas-revival/backend/internal/diskactivity"
)

type fakeMeasurer struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func (f *fakeMeasurer) Measure(ctx context.Context, seconds int) (diskactivity.Result, error) {
	if f.started != nil {
		close(f.started)
		<-f.release
	}
	return diskactivity.Result{Seconds: seconds}, f.err
}

func TestActivityValidatesSeconds(t *testing.T) {
	m := &ActivityManager{Measure: &fakeMeasurer{}}
	for _, s := range []int{0, 9, 46} {
		_, err := m.Run(context.Background(), s)
		if he, ok := AsError(err); !ok || he.Code != CodeInvalid || he.Field != "seconds" {
			t.Errorf("seconds=%d: err = %v", s, err)
		}
	}
	r, err := m.Run(context.Background(), 15)
	if err != nil || r.Seconds != 15 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
}

func TestActivityOneAtATime(t *testing.T) {
	f := &fakeMeasurer{started: make(chan struct{}), release: make(chan struct{})}
	m := &ActivityManager{Measure: f}
	done := make(chan error)
	go func() { _, err := m.Run(context.Background(), 10); done <- err }()
	<-f.started
	_, err := m.Run(context.Background(), 10)
	if he, ok := AsError(err); !ok || he.Code != CodeActivityRunning {
		t.Fatalf("second run: %v", err)
	}
	close(f.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("first run did not finish")
	}
}

func TestActivityHidesInternalError(t *testing.T) {
	m := &ActivityManager{Measure: &fakeMeasurer{err: errors.New("open /proc/diskstats: secret detail")}}
	_, err := m.Run(context.Background(), 10)
	if he, ok := AsError(err); !ok || he.Code != CodeInternal || he.Message != "Aktivitu disků nelze změřit." {
		t.Fatalf("err = %v", err)
	}
}

func TestDispatcherDiskActivity(t *testing.T) {
	d := &Dispatcher{}
	if _, err := d.Handle(context.Background(), request{Op: "disk.activity", Args: []byte(`{"seconds":10}`)}); err == nil {
		t.Fatal("op without manager must be unknown")
	}
	d.Activity = &ActivityManager{Measure: &fakeMeasurer{}}
	if _, err := d.Handle(context.Background(), request{Op: "disk.activity", Args: []byte(`{"seconds":10,"path":"/"}`)}); err == nil {
		t.Fatal("unknown argument accepted")
	}
	res, err := d.Handle(context.Background(), request{Op: "disk.activity", Args: []byte(`{"seconds":12}`)})
	if err != nil || res.(diskactivity.Result).Seconds != 12 {
		t.Fatalf("res=%v err=%v", res, err)
	}
}
