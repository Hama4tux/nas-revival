package helper

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nas-revival/backend/internal/services"
)

// Service is one allowlisted unit in the Services app (SVC-10).
type Service struct {
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	State       string     `json:"state"`
	SubState    string     `json:"subState"`
	Enabled     string     `json:"enabled"` // UnitFileState: enabled, disabled, static, masked, …
	Since       *time.Time `json:"since"`
	PID         int        `json:"pid,omitempty"`
	// MemoryBytes is the resident memory of every process in the unit's
	// cgroup (the NAS kernel has no memory controller, so systemd's
	// MemoryCurrent is not set).
	MemoryBytes *uint64 `json:"memoryBytes"`
	// CPUPercent is the share of the whole CPU since the previous list; nil
	// on the first list after the helper started or the unit restarted.
	CPUPercent *float64        `json:"cpuPercent"`
	Ports      []services.Port `json:"ports"`
	// Result and ExitStatus describe the last run when it did not succeed.
	Result     string   `json:"result,omitempty"`
	ExitStatus int      `json:"exitStatus,omitempty"`
	Protected  bool     `json:"protected"`
	Actions    []string `json:"actions"`
}

type ServiceList struct {
	Services []Service `json:"services"`
}

type ServiceLog struct {
	Name    string              `json:"name"`
	Entries []services.LogEntry `json:"entries"`
}

type cpuSample struct {
	ns uint64
	at time.Time
}

// ServiceManager serves service.list, service.action and journal.unit.
type ServiceManager struct {
	Run        Runner
	ProcRoot   string // /proc
	CgroupRoot string // /sys/fs/cgroup
	Now        func() time.Time
	NumCPU     int
	PageSize   int

	act    sync.Mutex // one action at a time
	mu     sync.Mutex // guards prev and lastPc
	prev   map[string]cpuSample
	lastPc map[string]float64
}

const (
	maxPortsPerUnit = 32
	// actionTimeout stays below the client timeout of service.action (45 s)
	// so a hanging stop still gets an answer.
	actionTimeout = 35 * time.Second
)

func (m *ServiceManager) show(ctx context.Context, names ...string) ([]services.Unit, error) {
	args := append([]string{"show", "--timestamp=unix", "--property=" + services.ShowProperties, "--"}, names...)
	raw, err := m.Run(ctx, "systemctl", args...)
	if err != nil && len(raw) == 0 {
		slog.Warn("helper: systemctl show failed", "err", err)
		return nil, internalErr("Stav služeb nelze zjistit.")
	}
	return services.ParseShow(raw), nil
}

// List reports every catalog unit; units systemd does not know are listed as
// not-installed so the UI can say so.
func (m *ServiceManager) List(ctx context.Context) (ServiceList, error) {
	names := make([]string, len(services.Catalog))
	for i, d := range services.Catalog {
		names[i] = d.Name
	}
	units, err := m.show(ctx, names...)
	if err != nil {
		return ServiceList{}, err
	}
	byID := map[string]services.Unit{}
	for _, u := range units {
		byID[u.ID] = u
	}
	ports := m.listeners(ctx)
	out := ServiceList{Services: make([]Service, 0, len(services.Catalog))}
	for _, d := range services.Catalog {
		out.Services = append(out.Services, m.build(d, byID[d.Name], ports))
	}
	return out, nil
}

func (m *ServiceManager) build(d services.Def, u services.Unit, ports []services.Listener) Service {
	s := Service{Name: d.Name, Label: d.Label, Description: u.Description, State: u.State(), SubState: u.SubState,
		Enabled: u.UnitFileState, Since: u.Since, Protected: d.Protected, Actions: u.Available(d), Ports: []services.Port{}}
	if !u.Installed() {
		s.Description, s.SubState, s.Enabled = "", "", ""
		return s
	}
	if u.Result != "" && u.Result != "success" {
		s.Result, s.ExitStatus = u.Result, u.ExecMainStatus
	}
	s.PID = u.MainPID
	pids := m.cgroupPIDs(u.ControlGroup)
	if len(pids) > 0 {
		var rss uint64
		for _, p := range pids {
			rss += m.rss(p)
		}
		s.MemoryBytes = &rss
		s.Ports = portsOf(pids, ports)
	}
	s.CPUPercent = m.cpuPercent(d.Name, u.CPUNSec)
	return s
}

// cpuPercent compares with the previous sample; samples closer than one
// second keep the last value so two quick lists do not show noise.
func (m *ServiceManager) cpuPercent(name string, ns *uint64) *float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.prev == nil {
		m.prev, m.lastPc = map[string]cpuSample{}, map[string]float64{}
	}
	if ns == nil {
		delete(m.prev, name)
		return nil
	}
	now := m.Now()
	p, ok := m.prev[name]
	if ok && now.Sub(p.at) < time.Second {
		if v, ok := m.lastPc[name]; ok {
			return &v
		}
		return nil
	}
	m.prev[name] = cpuSample{ns: *ns, at: now}
	if !ok || *ns < p.ns {
		delete(m.lastPc, name)
		return nil
	}
	cpus := m.NumCPU
	if cpus < 1 {
		cpus = 1
	}
	v := float64(*ns-p.ns) / float64(now.Sub(p.at).Nanoseconds()) / float64(cpus) * 100
	if v > 100 {
		v = 100
	}
	v = float64(int(v*10+0.5)) / 10
	m.lastPc[name] = v
	return &v
}

// cgroupPIDs reads cgroup.procs of the unit and its sub-cgroups. The path
// comes from systemd, but must stay inside CgroupRoot.
func (m *ServiceManager) cgroupPIDs(cg string) []int {
	if cg == "" || !strings.HasPrefix(cg, "/") || strings.Contains(cg, "..") {
		return nil
	}
	root := filepath.Join(m.CgroupRoot, cg)
	var pids []int
	filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || e.Name() != "cgroup.procs" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, f := range strings.Fields(string(b)) {
			if n, err := strconv.Atoi(f); err == nil && n > 0 {
				pids = append(pids, n)
			}
		}
		return nil
	})
	return pids
}

// rss reads the resident pages from /proc/<pid>/statm.
func (m *ServiceManager) rss(pid int) uint64 {
	b, err := os.ReadFile(filepath.Join(m.ProcRoot, strconv.Itoa(pid), "statm"))
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0
	}
	n, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return 0
	}
	return n * uint64(m.PageSize)
}

func (m *ServiceManager) listeners(ctx context.Context) []services.Listener {
	raw, err := m.Run(ctx, "ss", "-H", "-l", "-t", "-u", "-n", "-p")
	if err != nil {
		slog.Warn("helper: ss failed", "err", err)
		return nil
	}
	return services.ParseSS(raw)
}

func portsOf(pids []int, ls []services.Listener) []services.Port {
	in := map[int]bool{}
	for _, p := range pids {
		in[p] = true
	}
	out := []services.Port{}
	seen := map[services.Port]bool{}
	for _, l := range ls {
		for _, p := range l.PIDs {
			if in[p] && !seen[l.Port] && len(out) < maxPortsPerUnit {
				seen[l.Port] = true
				out = append(out, l.Port)
				break
			}
		}
	}
	return out
}

func lookupUnit(name string) (services.Def, error) {
	d, ok := services.Lookup(name)
	if !ok {
		return services.Def{}, &Error{Code: CodeNotFound, Message: "Tato služba se nespravuje.", Field: "name"}
	}
	return d, nil
}

// Action starts, stops, restarts, reloads, enables or disables one catalog
// unit (SVC-11) and returns its new state. user is recorded in the log.
func (m *ServiceManager) Action(ctx context.Context, name, action, user string) (Service, error) {
	d, err := lookupUnit(name)
	if err != nil {
		return Service{}, err
	}
	if !d.Allows(action) {
		msg := "Tuto akci služba nepodporuje."
		if d.Protected {
			msg = "Služba je chráněná: zastavit ani vypnout ji z webové správy nejde."
		}
		return Service{}, invalid("action", msg)
	}
	m.act.Lock()
	defer m.act.Unlock()
	units, err := m.show(ctx, name)
	if err != nil {
		return Service{}, err
	}
	if len(units) == 0 || !units[0].Installed() {
		return Service{}, &Error{Code: CodeNotFound, Message: "Služba není nainstalovaná.", Field: "name"}
	}
	if action == services.ActReload && !units[0].CanReload {
		return Service{}, invalid("action", "Služba neumí znovu načíst nastavení.")
	}
	actx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	if _, err := m.Run(actx, "systemctl", action, "--", name); err != nil {
		slog.Warn("helper: service."+action+" failed", "unit", name, "user", user, "err", err)
		// The unit may have changed anyway (failed start); the UI reloads the list.
		return Service{}, &Error{Code: CodeInternal, Message: "Akce se nepovedla. Podrobnosti jsou v záznamu služby."}
	}
	slog.Info("helper: service."+action, "unit", name, "user", user)
	units, err = m.show(ctx, name)
	if err != nil || len(units) == 0 {
		return Service{}, internalErr("Akce proběhla, ale nový stav nelze zjistit.")
	}
	return m.build(d, units[0], m.listeners(ctx)), nil
}

// Log returns the last lines of one catalog unit's journal (SVC-12).
func (m *ServiceManager) Log(ctx context.Context, name string, lines int) (ServiceLog, error) {
	if _, err := lookupUnit(name); err != nil {
		return ServiceLog{}, err
	}
	if lines < 1 || lines > services.MaxLogLines {
		return ServiceLog{}, invalid("lines", "Počet řádků musí být 1 až 200.")
	}
	raw, err := m.Run(ctx, "journalctl", "--unit="+name, "--lines="+strconv.Itoa(lines), "--no-pager", "--quiet",
		"--output=json", "--output-fields=PRIORITY,MESSAGE,SYSLOG_IDENTIFIER,_PID")
	if err != nil && len(raw) == 0 {
		slog.Warn("helper: journalctl failed", "unit", name, "err", err)
		return ServiceLog{}, internalErr("Záznam služby nelze načíst.")
	}
	return ServiceLog{Name: name, Entries: services.ParseJournal(raw)}, nil
}
