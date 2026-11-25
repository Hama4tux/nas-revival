package diskpower

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

// Runner runs a command and returns its standard output and error together.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs hdparm with a fixed environment and a time limit. Standard
// error is included because hdparm reports a failed pass-through only there.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	return cmd.CombinedOutput()
}

// Live is the drive's current power state (PWR-01).
type Live struct {
	State     string    `json:"state"` // active, standby, sleeping, unknown
	CheckedAt time.Time `json:"checkedAt"`
	// APM is read only from an awake drive (IDENTIFY may spin a sleeping one
	// up); while it sleeps the last value read is reported.
	APMSupported *bool `json:"apmSupported"`
	APMLevel     *int  `json:"apmLevel"`
}

type apmInfo struct {
	supported bool
	level     *int
}

// Reader asks the drive with hdparm -C, which never wakes it.
type Reader struct {
	Run        Runner
	Now        func() time.Time
	ConfigPath string
	mu         sync.Mutex
	apm        map[string]apmInfo
}

func (r *Reader) Read(ctx context.Context, device string) Live {
	dev := "/dev/" + device
	l := Live{State: StateUnknown, CheckedAt: r.Now()}
	if out, err := r.Run(ctx, "hdparm", "-C", dev); err == nil {
		l.State = ParseState(string(out))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.apm == nil {
		r.apm = map[string]apmInfo{}
	}
	if l.State == StateActive {
		if out, err := r.Run(ctx, "hdparm", "-B", dev); err == nil {
			if level, sup, ok := ParseAPM(string(out)); ok {
				r.apm[device] = apmInfo{supported: sup, level: level}
			}
		}
	}
	if a, ok := r.apm[device]; ok {
		sup := a.supported
		l.APMSupported, l.APMLevel = &sup, a.level
	}
	return l
}

// Config reads the stored settings (written by the helper).
func (r *Reader) Config() (Config, error) { return Load(r.ConfigPath) }
