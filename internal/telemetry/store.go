package telemetry

import (
	"context"
	"sync"
	"sync/atomic"
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

type Store struct {
	capacity int
	ttl      time.Duration

	mu     sync.RWMutex
	events []Event

	inFlight      atomic.Int64
	totalRequests atomic.Int64
	totalErrors   atomic.Int64
	bytesIn       atomic.Int64
	bytesOut      atomic.Int64
}

func NewStore(capacity int, ttl time.Duration) *Store {
	if capacity <= 0 {
		capacity = 10000
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	return &Store{capacity: capacity, ttl: ttl, events: make([]Event, 0, min(capacity, 128))}
}

func (s *Store) Start(requestBytes int64) {
	s.inFlight.Add(1)
	s.totalRequests.Add(1)
	if requestBytes > 0 {
		s.bytesIn.Add(requestBytes)
	}
}

func (s *Store) Finish(result Result) {
	s.inFlight.Add(-1)
	if result.ResponseBytes > 0 {
		s.bytesOut.Add(result.ResponseBytes)
	}
	if result.Err != nil || result.Status >= 500 {
		s.totalErrors.Add(1)
	}

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

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now())
	s.events = append(s.events, event)
	if over := len(s.events) - s.capacity; over > 0 {
		s.events = append([]Event(nil), s.events[over:]...)
	}
}

func (s *Store) Snapshot() Stats {
	return Stats{
		InFlight:      s.inFlight.Load(),
		TotalRequests: s.totalRequests.Load(),
		TotalErrors:   s.totalErrors.Load(),
		BytesIn:       s.bytesIn.Load(),
		BytesOut:      s.bytesOut.Load(),
	}
}

func (s *Store) Recent(limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.events) {
		limit = len(s.events)
	}
	start := len(s.events) - limit
	out := make([]Event, limit)
	copy(out, s.events[start:])
	return out
}

func (s *Store) CleanupExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(now)
}

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

func (s *Store) cleanupExpiredLocked(now time.Time) {
	if len(s.events) == 0 {
		return
	}
	cutoff := now.Add(-s.ttl)
	idx := 0
	for idx < len(s.events) && s.events[idx].Time.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		s.events = append([]Event(nil), s.events[idx:]...)
	}
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
