package telemetry

import (
	"context"
	"runtime"
	"sync"
	"time"
)

type Sample struct {
	Time           time.Time `json:"time"`
	CPUPercent     float64   `json:"cpu_percent"`
	MemoryBytes    int64     `json:"memory_bytes"`
	MemoryPercent  float64   `json:"memory_percent"`
	BytesIn        int64     `json:"bytes_in"`
	BytesOut       int64     `json:"bytes_out"`
	Connections    int64     `json:"connections"`
	TotalRequests  int64     `json:"total_requests"`
	TotalErrors    int64     `json:"total_errors"`
	Goroutines     int       `json:"goroutines"`
	HeapAllocBytes int64     `json:"heap_alloc_bytes"`
}

type SysStatsCollector struct {
	mu       sync.RWMutex
	samples  []Sample
	capacity int
	interval time.Duration
	window   time.Duration

	statsFn func() Stats

	lastCPURead cpuSample
}

type cpuSample struct {
	user    uint64
	system  uint64
	idle    uint64
	nice    uint64
	stolen  uint64
	total   uint64
	sampled time.Time
}

func NewSysStatsCollector(statsFn func() Stats, capacity int, interval, window time.Duration) *SysStatsCollector {
	if capacity <= 0 {
		capacity = 360 // 1h at 10s
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if window <= 0 {
		window = time.Hour
	}
	if statsFn == nil {
		statsFn = func() Stats { return Stats{} }
	}
	return &SysStatsCollector{
		samples:  make([]Sample, 0, min(capacity, 64)),
		capacity: capacity,
		interval: interval,
		window:   window,
		statsFn:  statsFn,
	}
}

// Start begins the background sampling goroutine.
func (c *SysStatsCollector) Start(ctx context.Context) {
	c.lastCPURead = readCPU()
	c.lastCPURead.sampled = time.Now()

	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				sample := c.collect(now)
				c.push(sample)
			}
		}
	}()
}

// History returns all stored samples within the given duration from now.
// If since is zero, returns all stored samples.
func (c *SysStatsCollector) History(since time.Duration) []Sample {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	if since <= 0 {
		out := make([]Sample, len(c.samples))
		copy(out, c.samples)
		return out
	}

	cutoff := now.Add(-since)
	start := 0
	for start < len(c.samples) && c.samples[start].Time.Before(cutoff) {
		start++
	}
	out := make([]Sample, len(c.samples)-start)
	copy(out, c.samples[start:])
	return out
}

func (c *SysStatsCollector) push(s Sample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked(s.Time)
	c.samples = append(c.samples, s)
	if over := len(c.samples) - c.capacity; over > 0 {
		c.samples = append([]Sample(nil), c.samples[over:]...)
	}
}

func (c *SysStatsCollector) cleanupLocked(now time.Time) {
	if len(c.samples) == 0 {
		return
	}
	cutoff := now.Add(-c.window)
	idx := 0
	for idx < len(c.samples) && c.samples[idx].Time.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		c.samples = append([]Sample(nil), c.samples[idx:]...)
	}
}

func (c *SysStatsCollector) collect(now time.Time) Sample {
	curCPU := readCPU()
	cpuPercent := calcCPUPercent(c.lastCPURead, curCPU)
	c.lastCPURead = curCPU

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	stats := c.statsFn()

	return Sample{
		Time:           now,
		CPUPercent:     round1(cpuPercent),
		MemoryBytes:    int64(m.Sys),
		MemoryPercent:  calcMemoryPercent(m),
		BytesIn:        stats.BytesIn,
		BytesOut:       stats.BytesOut,
		Connections:    stats.InFlight,
		TotalRequests:  stats.TotalRequests,
		TotalErrors:    stats.TotalErrors,
		Goroutines:     runtime.NumGoroutine(),
		HeapAllocBytes: int64(m.HeapAlloc),
	}
}

func calcCPUPercent(prev, cur cpuSample) float64 {
	prevTotal := prev.user + prev.system + prev.idle + prev.nice + prev.stolen
	curTotal := cur.user + cur.system + cur.idle + cur.nice + cur.stolen
	diffTotal := curTotal - prevTotal
	if diffTotal == 0 {
		return 0
	}
	diffBusy := (cur.user + cur.system + cur.nice + cur.stolen) - (prev.user + prev.system + prev.nice + prev.stolen)
	return float64(diffBusy) / float64(diffTotal) * 100.0
}

func calcMemoryPercent(m runtime.MemStats) float64 {
	// On most OSes we can't easily get total system RAM without CGO.
	// We report percent of Go runtime's view (HeapAlloc / Sys) as a rough gauge.
	if m.Sys == 0 {
		return 0
	}
	return round1(float64(m.HeapAlloc) / float64(m.Sys) * 100.0)
}

func round1(v float64) float64 {
	const prec = 100
	return float64(int(v*prec+0.5)) / prec
}
