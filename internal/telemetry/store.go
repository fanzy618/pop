// Package telemetry collects per-request observability data. Store is the
// composition root that ties together three single-purpose pieces — atomic
// counters, a bounded TTL event ring buffer, and a fan-out event bus —
// behind one public type with stable method signatures.
package telemetry

import (
	"context"
	"time"
)

type Event struct {
	Time          time.Time `json:"time"`
	Client        string    `json:"client"`
	Method        string    `json:"method"`
	Host          string    `json:"host"`
	Action        string    `json:"action"`
	RuleID        string    `json:"rule_id,omitempty"`
	Status        int       `json:"status"`
	DurationMS    int64     `json:"duration_ms"`
	RequestBytes  int64     `json:"request_bytes"`
	ResponseBytes int64     `json:"response_bytes"`
	Error         string    `json:"error,omitempty"`
}

type Result struct {
	Client        string
	Method        string
	Host          string
	Action        string
	RuleID        string
	Status        int
	Duration      time.Duration
	RequestBytes  int64
	ResponseBytes int64
	Err           error
}

type Stats struct {
	InFlight      int64 `json:"in_flight"`
	TotalRequests int64 `json:"total_requests"`
	TotalErrors   int64 `json:"total_errors"`
	BytesIn       int64 `json:"bytes_in"`
	BytesOut      int64 `json:"bytes_out"`
}

// Store composes counters + eventBuffer + eventBus. Public API and JSON
// wire format are unchanged from the prior monolithic implementation —
// only internal cohesion has improved.
type Store struct {
	counters counters
	buffer   *eventBuffer
	bus      *eventBus
}

func NewStore(capacity int, ttl time.Duration) *Store {
	if capacity <= 0 {
		capacity = 10000
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &Store{
		buffer: newEventBuffer(capacity, ttl),
		bus:    newEventBus(),
	}
}

func (s *Store) Start(requestBytes int64) {
	s.counters.start(requestBytes)
}

func (s *Store) Finish(result Result) {
	s.counters.finish(result.ResponseBytes, result.Err != nil || result.Status >= 500)

	event := Event{
		Time:          time.Now(),
		Client:        result.Client,
		Method:        result.Method,
		Host:          result.Host,
		Action:        result.Action,
		RuleID:        result.RuleID,
		Status:        result.Status,
		DurationMS:    result.Duration.Milliseconds(),
		RequestBytes:  max64(result.RequestBytes, 0),
		ResponseBytes: max64(result.ResponseBytes, 0),
	}
	if result.Err != nil {
		event.Error = result.Err.Error()
	}

	s.buffer.append(event)
	s.bus.publish(event)
}

func (s *Store) Snapshot() Stats               { return s.counters.snapshot() }
func (s *Store) Recent(limit int) []Event      { return s.buffer.recent(limit) }
func (s *Store) CleanupExpired(now time.Time)  { s.buffer.cleanupExpired(now) }
func (s *Store) Subscribe(buffer int) (<-chan Event, func()) {
	return s.bus.subscribe(buffer)
}

// StartJanitor periodically evicts events older than the TTL. The goroutine
// exits when ctx is cancelled.
func (s *Store) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				s.CleanupExpired(now)
			}
		}
	}()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
