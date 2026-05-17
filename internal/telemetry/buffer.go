package telemetry

import (
	"sync"
	"time"
)

// eventBuffer is a bounded, TTL-evicting ring of recent events. Append and
// read paths are serialized by a single RWMutex; readers obtain a copy so
// they can't observe a partially-trimmed slice.
type eventBuffer struct {
	capacity int
	ttl      time.Duration

	mu     sync.RWMutex
	events []Event
}

func newEventBuffer(capacity int, ttl time.Duration) *eventBuffer {
	return &eventBuffer{
		capacity: capacity,
		ttl:      ttl,
		events:   make([]Event, 0, min(capacity, 128)),
	}
}

func (b *eventBuffer) append(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupExpiredLocked(time.Now())
	b.events = append(b.events, ev)
	if over := len(b.events) - b.capacity; over > 0 {
		b.events = append([]Event(nil), b.events[over:]...)
	}
}

func (b *eventBuffer) recent(limit int) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if limit <= 0 || limit > len(b.events) {
		limit = len(b.events)
	}
	start := len(b.events) - limit
	out := make([]Event, limit)
	copy(out, b.events[start:])
	return out
}

func (b *eventBuffer) cleanupExpired(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupExpiredLocked(now)
}

func (b *eventBuffer) cleanupExpiredLocked(now time.Time) {
	if len(b.events) == 0 {
		return
	}
	cutoff := now.Add(-b.ttl)
	idx := 0
	for idx < len(b.events) && b.events[idx].Time.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		b.events = append([]Event(nil), b.events[idx:]...)
	}
}
