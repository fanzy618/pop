package telemetry

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultConnectionsCap is the upper bound on entries the registry holds.
// Past the cap, new requests are still served but not tracked.
const DefaultConnectionsCap = 4096

// ConnState is the live, mutable view of one in-flight request. Byte
// counters are atomic so the proxy data path updates them without holding
// the registry lock.
type ConnState struct {
	ID         uint64
	StartedAt  time.Time
	Client     string
	Method     string
	Host       string
	Action     string
	RuleID     string
	UpstreamID string
	BytesIn    atomic.Int64
	BytesOut   atomic.Int64
}

// ConnSnapshot is the wire-friendly copy returned to readers (REST handler,
// tests). It carries plain ints rather than atomics so it can be marshalled
// and ranged over without surprises.
type ConnSnapshot struct {
	ID         uint64    `json:"id"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS int64     `json:"duration_ms"`
	Client     string    `json:"client"`
	Method     string    `json:"method"`
	Host       string    `json:"host"`
	Action     string    `json:"action"`
	RuleID     string    `json:"rule_id,omitempty"`
	UpstreamID string    `json:"upstream_id,omitempty"`
	BytesIn    int64     `json:"bytes_in"`
	BytesOut   int64     `json:"bytes_out"`
}

// Connections is the in-memory registry of in-flight requests. Safe for
// concurrent use. Bounded: once `cap` entries are tracked, Open returns nil
// and the caller proceeds without instrumentation.
type Connections struct {
	cap    int
	nextID atomic.Uint64

	mu     sync.RWMutex
	active map[uint64]*ConnState
}

// NewConnections returns a registry with the given cap; if cap <= 0,
// DefaultConnectionsCap is used.
func NewConnections(cap int) *Connections {
	if cap <= 0 {
		cap = DefaultConnectionsCap
	}
	return &Connections{cap: cap, active: make(map[uint64]*ConnState)}
}

// Open registers a new connection. The returned *ConnState's byte counters
// are the live atomics for the proxy to increment. seed.ID and seed.StartedAt
// are overwritten with fresh values. Returns nil if the registry is at cap.
func (c *Connections) Open(seed ConnState) *ConnState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.active) >= c.cap {
		return nil
	}
	id := c.nextID.Add(1)
	state := &ConnState{
		ID:         id,
		StartedAt:  time.Now(),
		Client:     seed.Client,
		Method:     seed.Method,
		Host:       seed.Host,
		Action:     seed.Action,
		RuleID:     seed.RuleID,
		UpstreamID: seed.UpstreamID,
	}
	c.active[id] = state
	return state
}

// Close removes id from the registry. Safe to call on an unknown id.
func (c *Connections) Close(id uint64) {
	c.mu.Lock()
	delete(c.active, id)
	c.mu.Unlock()
}

// Snapshot returns the active connections, oldest first, copied to wire-safe
// structs. Computing duration here (server-side) avoids client clock skew.
func (c *Connections) Snapshot() []ConnSnapshot {
	now := time.Now()
	c.mu.RLock()
	out := make([]ConnSnapshot, 0, len(c.active))
	for _, s := range c.active {
		out = append(out, ConnSnapshot{
			ID:         s.ID,
			StartedAt:  s.StartedAt,
			DurationMS: now.Sub(s.StartedAt).Milliseconds(),
			Client:     s.Client,
			Method:     s.Method,
			Host:       s.Host,
			Action:     s.Action,
			RuleID:     s.RuleID,
			UpstreamID: s.UpstreamID,
			BytesIn:    s.BytesIn.Load(),
			BytesOut:   s.BytesOut.Load(),
		})
	}
	c.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Len returns the current number of tracked connections.
func (c *Connections) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.active)
}
