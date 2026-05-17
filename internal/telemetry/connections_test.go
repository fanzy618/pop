package telemetry

import (
	"sync"
	"testing"
	"time"
)

func TestConnections_OpenSnapshotClose(t *testing.T) {
	t.Parallel()

	c := NewConnections(0)
	if c.Len() != 0 {
		t.Fatalf("fresh registry should be empty")
	}

	state := c.Open(ConnState{Client: "1.2.3.4:1000", Method: "GET", Host: "example.com", Action: "DIRECT"})
	if state == nil {
		t.Fatalf("Open returned nil under cap")
	}
	state.BytesIn.Store(100)
	state.BytesOut.Store(200)

	snap := c.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len=%d, want 1", len(snap))
	}
	s := snap[0]
	if s.ID != state.ID || s.Client != "1.2.3.4:1000" || s.Host != "example.com" {
		t.Fatalf("snapshot fields off: %+v", s)
	}
	if s.BytesIn != 100 || s.BytesOut != 200 {
		t.Fatalf("byte counters not reflected: in=%d out=%d", s.BytesIn, s.BytesOut)
	}
	if s.DurationMS < 0 {
		t.Fatalf("duration negative: %d", s.DurationMS)
	}

	c.Close(state.ID)
	if c.Len() != 0 {
		t.Fatalf("after Close, len=%d", c.Len())
	}
}

func TestConnections_CapRejectsBeyondLimit(t *testing.T) {
	t.Parallel()

	c := NewConnections(3)
	for i := 0; i < 3; i++ {
		if c.Open(ConnState{Host: "h"}) == nil {
			t.Fatalf("Open %d should succeed under cap", i)
		}
	}
	if c.Open(ConnState{Host: "overflow"}) != nil {
		t.Fatalf("Open beyond cap should return nil")
	}
	if c.Len() != 3 {
		t.Fatalf("len=%d, want 3", c.Len())
	}
}

func TestConnections_SnapshotOrderByIDAscending(t *testing.T) {
	t.Parallel()

	c := NewConnections(0)
	a := c.Open(ConnState{Host: "a"})
	time.Sleep(time.Millisecond)
	b := c.Open(ConnState{Host: "b"})
	time.Sleep(time.Millisecond)
	d := c.Open(ConnState{Host: "d"})

	snap := c.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("snap len=%d", len(snap))
	}
	if snap[0].ID != a.ID || snap[1].ID != b.ID || snap[2].ID != d.ID {
		t.Fatalf("snap order=[%d,%d,%d] want [%d,%d,%d]",
			snap[0].ID, snap[1].ID, snap[2].ID, a.ID, b.ID, d.ID)
	}
}

func TestConnections_ConcurrentOpenCloseSnapshot_RaceClean(t *testing.T) {
	t.Parallel()

	c := NewConnections(1000)
	const writers = 16
	const ops = 200

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				s := c.Open(ConnState{Host: "h", Method: "GET"})
				if s == nil {
					continue
				}
				s.BytesIn.Add(1)
				s.BytesOut.Add(2)
				c.Close(s.ID)
			}
		}()
	}

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = c.Snapshot()
			}
		}
	}()

	wg.Wait()
	close(stop)
	if c.Len() != 0 {
		t.Fatalf("len=%d after all Close, want 0", c.Len())
	}
}
