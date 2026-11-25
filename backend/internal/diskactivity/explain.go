package diskactivity

import (
	"sort"
	"strconv"
	"strings"
)

// DiskInfo is what the web process knows about a disk from /storage and disks.json.
type DiskInfo struct {
	Name           string
	ID             string
	System         bool
	SharedWithData bool
	// StandbyMinutes is the configured spin-down timeout, nil when unknown
	// (disk without power management).
	StandbyMinutes *int
}

// DiskSummary groups a measurement by whole disk with the reasons it stays awake.
type DiskSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	System bool   `json:"system"`
	DevStatDelta
	Partitions []DeviceActivity `json:"partitions"`
	Hints      []Hint           `json:"hints"`
}

// Hint is one finding; the UI translates Code, Value fills a placeholder.
type Hint struct {
	Code  string `json:"code"`
	Value string `json:"value,omitempty"`
}

const (
	HintIdle         = "idle"         // no I/O at all in the window
	HintNoStandby    = "no_standby"   // no spin-down timeout configured
	HintWritesWake   = "writes_wake"  // writes more often than the timeout (Value: minutes)
	HintJournald     = "journald"     // systemd journal written
	HintSwap         = "swap"         // swap partition used
	HintFSJournal    = "fs_journal"   // ext4 journal commits without a changed file (Value: partition)
	HintUnattributed = "unattributed" // writes that no file explains
	HintSharedData   = "shared_data"  // system writes keep the data partition awake too
	journalDir       = "/var/log/journal/"
	journaldComm     = "systemd-journal"
)

// Explain groups devices by disk and adds hints. Disks the web process does
// not know (no DiskInfo) are still listed, without an ID.
func Explain(r Result, disks []DiskInfo) []DiskSummary {
	info := map[string]DiskInfo{}
	for _, d := range disks {
		info[d.Name] = d
	}
	byDisk := map[string]*DiskSummary{}
	var order []string
	get := func(name string) *DiskSummary {
		if s, ok := byDisk[name]; ok {
			return s
		}
		di := info[name]
		s := &DiskSummary{ID: di.ID, Name: name, System: di.System, Partitions: []DeviceActivity{}, Hints: []Hint{}}
		byDisk[name] = s
		order = append(order, name)
		return s
	}
	for _, d := range r.Devices {
		if d.Parent == "" {
			s := get(d.Name)
			s.DevStatDelta = d.DevStatDelta
			continue
		}
		s := get(d.Parent)
		s.Partitions = append(s.Partitions, d)
	}
	filesOn := map[string][]FileActivity{}
	for _, f := range r.Files {
		filesOn[f.Device] = append(filesOn[f.Device], f)
	}
	sort.Strings(order)
	out := make([]DiskSummary, 0, len(order))
	for _, name := range order {
		s := byDisk[name]
		s.Hints = hints(*s, info[name], filesOn)
		out = append(out, *s)
	}
	return out
}

func hints(s DiskSummary, di DiskInfo, filesOn map[string][]FileActivity) []Hint {
	h := []Hint{}
	if s.ReadOps+s.WriteOps == 0 {
		h = append(h, Hint{Code: HintIdle})
	}
	if di.StandbyMinutes != nil && *di.StandbyMinutes == 0 {
		h = append(h, Hint{Code: HintNoStandby})
	} else if di.StandbyMinutes != nil && s.WriteOps > 0 {
		h = append(h, Hint{Code: HintWritesWake, Value: strconv.Itoa(*di.StandbyMinutes)})
	}
	journald, swap, explained := false, false, false
	var fsOnly []string
	for _, p := range s.Partitions {
		if p.Swap && p.ReadOps+p.WriteOps > 0 {
			swap = true
		}
		files := filesOn[p.Name]
		if len(files) > 0 {
			explained = true
		}
		for _, f := range files {
			if strings.HasPrefix(f.Path, journalDir) || heldBy(f, journaldComm) {
				journald = true
			}
		}
		if p.JournalCommits != nil && *p.JournalCommits > 0 && len(files) == 0 {
			fsOnly = append(fsOnly, p.Name)
		}
	}
	if journald {
		h = append(h, Hint{Code: HintJournald})
	}
	if swap {
		h = append(h, Hint{Code: HintSwap})
	}
	for _, p := range fsOnly {
		h = append(h, Hint{Code: HintFSJournal, Value: p})
	}
	if s.WriteOps > 0 && !explained && !swap && len(fsOnly) == 0 {
		h = append(h, Hint{Code: HintUnattributed})
	}
	if di.SharedWithData && s.WriteOps > 0 {
		h = append(h, Hint{Code: HintSharedData})
	}
	return h
}

func heldBy(f FileActivity, comm string) bool {
	for _, p := range f.Processes {
		if strings.HasPrefix(p, comm+" (") {
			return true
		}
	}
	return false
}
