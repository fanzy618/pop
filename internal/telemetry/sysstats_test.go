package telemetry

import (
	"runtime"
	"testing"
	"time"
)

func TestSysStatsCollectorCapacity(t *testing.T) {
	t.Parallel()

	c := NewSysStatsCollector(func() Stats { return Stats{} }, 5, time.Second, time.Hour)
	base := time.Now().Add(-10 * time.Second)
	for i := 0; i < 10; i++ {
		c.push(Sample{Time: base.Add(time.Duration(i) * time.Second), TotalRequests: int64(i)})
	}

	h := c.History(0)
	if len(h) != 5 {
		t.Fatalf("history len = %d, want 5", len(h))
	}
	for i := 0; i < 5; i++ {
		want := int64(i + 5)
		if h[i].TotalRequests != want {
			t.Fatalf("history[%d].TotalRequests = %d, want %d", i, h[i].TotalRequests, want)
		}
	}
}

func TestSysStatsCollectorWindow(t *testing.T) {
	t.Parallel()

	c := NewSysStatsCollector(func() Stats { return Stats{} }, 10, time.Second, 100*time.Millisecond)
	c.push(Sample{Time: time.Now(), TotalRequests: 1})
	time.Sleep(150 * time.Millisecond)
	latest := Sample{Time: time.Now(), TotalRequests: 2}
	c.push(latest)

	h := c.History(100 * time.Millisecond)
	if len(h) != 1 {
		t.Fatalf("history len = %d, want 1", len(h))
	}
	if h[0].TotalRequests != latest.TotalRequests {
		t.Fatalf("history[0].TotalRequests = %d, want %d", h[0].TotalRequests, latest.TotalRequests)
	}
}

func TestSysStatsCollectorHistorySince(t *testing.T) {
	t.Parallel()

	c := NewSysStatsCollector(func() Stats { return Stats{} }, 10, time.Second, time.Hour)
	base := time.Now().Add(-4 * time.Second)
	for i := 0; i < 5; i++ {
		c.push(Sample{Time: base.Add(time.Duration(i) * time.Second), TotalRequests: int64(i)})
	}

	h := c.History(2 * time.Second)
	if len(h) < 2 || len(h) > 3 {
		t.Fatalf("history len = %d, want 2 or 3", len(h))
	}
	for i := 1; i < len(h); i++ {
		if h[i].Time.Before(h[i-1].Time) {
			t.Fatalf("history not sorted by time ascending")
		}
	}
}

func TestSysStatsCollectorNilStatsFn(t *testing.T) {
	t.Parallel()

	c := NewSysStatsCollector(nil, 10, time.Second, time.Hour)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("collect panicked: %v", r)
		}
	}()

	_ = c.collect(time.Now())
}

func TestCalcCPUPercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		prev cpuSample
		cur  cpuSample
		want float64
	}{
		{
			name: "known values",
			prev: cpuSample{user: 100, system: 50, idle: 850, nice: 0, stolen: 0},
			cur:  cpuSample{user: 200, system: 100, idle: 1700, nice: 0, stolen: 0},
			want: 15,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := calcCPUPercent(tc.prev, tc.cur)
			if got != tc.want {
				t.Fatalf("calcCPUPercent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCalcCPUPercentZeroDelta(t *testing.T) {
	t.Parallel()

	v := cpuSample{user: 1, system: 2, idle: 3, nice: 4, stolen: 5}
	got := calcCPUPercent(v, v)
	if got != 0 {
		t.Fatalf("calcCPUPercent() = %v, want 0", got)
	}
}

func TestCalcMemoryPercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    runtime.MemStats
		want float64
	}{
		{
			name: "known values",
			m:    runtime.MemStats{Sys: 100, HeapAlloc: 25},
			want: 25,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := calcMemoryPercent(tc.m)
			if got != tc.want {
				t.Fatalf("calcMemoryPercent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCalcMemoryPercentZeroSys(t *testing.T) {
	t.Parallel()

	got := calcMemoryPercent(runtime.MemStats{Sys: 0, HeapAlloc: 25})
	if got != 0 {
		t.Fatalf("calcMemoryPercent() = %v, want 0", got)
	}
}

func TestSysStatsCollectorDefaults(t *testing.T) {
	t.Parallel()

	c := NewSysStatsCollector(nil, 0, 0, 0)
	if c.capacity != 360 {
		t.Fatalf("capacity = %d, want 360", c.capacity)
	}
	if c.interval != 10*time.Second {
		t.Fatalf("interval = %v, want %v", c.interval, 10*time.Second)
	}
	if c.window != time.Hour {
		t.Fatalf("window = %v, want %v", c.window, time.Hour)
	}

	c = NewSysStatsCollector(nil, -1, -1, -1)
	if c.capacity != 360 {
		t.Fatalf("capacity (negative input) = %d, want 360", c.capacity)
	}
	if c.interval != 10*time.Second {
		t.Fatalf("interval (negative input) = %v, want %v", c.interval, 10*time.Second)
	}
	if c.window != time.Hour {
		t.Fatalf("window (negative input) = %v, want %v", c.window, time.Hour)
	}
}
