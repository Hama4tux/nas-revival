package hardware

import (
	"context"
	"sync"
	"time"
)

// Reading is one sample of the sensors: temperatures in °C by sensor ID
// (hwmon "lm85/temp1", disks "disk/sda"), the fan speed and PWM.
type Reading struct {
	Temps      map[string]float64
	FanRPM     *int64
	PWMPercent *int
}

// Point is one minute of history: the highest temperature of the minute (a
// short spike must not be averaged away), the last fan speed and PWM.
type Point struct {
	T          int64              `json:"t"` // Unix ms, start of the minute
	Temps      map[string]float64 `json:"temps"`
	FanRPM     *int64             `json:"fanRpm"`
	PWMPercent *int               `json:"pwmPercent"`
}

// History keeps 24 h of minute points in RAM (~1 440 points, ~150 KiB).
type History struct {
	Read     func(ctx context.Context) Reading
	Interval time.Duration // sampling, 10 s
	Size     int
	Now      func() time.Time

	mu   sync.Mutex
	ring []Point
	next int
	full bool
	cur  *Point // the minute being filled, not yet in ring
}

func NewHistory(read func(ctx context.Context) Reading) *History {
	return &History{Read: read, Interval: 10 * time.Second, Size: 24 * 60, Now: time.Now}
}

func (h *History) Run(ctx context.Context) {
	h.Add(h.Now(), h.Read(ctx))
	t := time.NewTicker(h.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Add(h.Now(), h.Read(ctx))
		}
	}
}

// Add folds a reading into its minute; a new minute closes the previous one.
func (h *History) Add(at time.Time, r Reading) {
	minute := at.Truncate(time.Minute).UnixMilli()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur != nil && h.cur.T != minute {
		h.push(*h.cur)
		h.cur = nil
	}
	if h.cur == nil {
		h.cur = &Point{T: minute, Temps: map[string]float64{}}
	}
	for id, v := range r.Temps {
		if old, ok := h.cur.Temps[id]; !ok || v > old {
			h.cur.Temps[id] = v
		}
	}
	if r.FanRPM != nil {
		h.cur.FanRPM = r.FanRPM
	}
	if r.PWMPercent != nil {
		h.cur.PWMPercent = r.PWMPercent
	}
}

func (h *History) push(p Point) {
	if h.ring == nil {
		h.ring = make([]Point, h.Size)
	}
	h.ring[h.next] = p
	h.next = (h.next + 1) % h.Size
	if h.next == 0 {
		h.full = true
	}
}

// Since returns the closed minutes newer than sinceMs plus the running one,
// oldest first. The running minute changes until it closes, so a poller asks
// with since = the last closed point it has.
func (h *History) Since(sinceMs int64) []Point {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Point{}
	start, count := 0, h.next
	if h.full {
		start, count = h.next, h.Size
	}
	for i := 0; i < count; i++ {
		if p := h.ring[(start+i)%h.Size]; p.T > sinceMs {
			out = append(out, clonePoint(p))
		}
	}
	if h.cur != nil && h.cur.T > sinceMs {
		out = append(out, clonePoint(*h.cur))
	}
	return out
}

func clonePoint(p Point) Point {
	m := make(map[string]float64, len(p.Temps))
	for k, v := range p.Temps {
		m[k] = v
	}
	p.Temps = m
	return p
}
