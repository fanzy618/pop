package telemetry

import (
	"errors"
	"testing"
	"time"
)

func TestStoreCapacityBounded(t *testing.T) {
	t.Parallel()

	store := NewStore(3, time.Hour)
	for i := 0; i < 5; i++ {
		store.Start(10)
		store.Finish(Result{Status: 200, Duration: time.Millisecond})
	}

	events := store.Recent(10)
	if len(events) != 3 {
		t.Fatalf("events length = %d, want 3", len(events))
	}
}

func TestStoreTTLCleanup(t *testing.T) {
	t.Parallel()

	store := NewStore(10, 50*time.Millisecond)
	store.Start(0)
	store.Finish(Result{Status: 200, Duration: time.Millisecond})

	time.Sleep(80 * time.Millisecond)
	store.CleanupExpired(time.Now())

	events := store.Recent(10)
	if len(events) != 0 {
		t.Fatalf("events length = %d, want 0 after ttl cleanup", len(events))
	}
}

func TestStoreStats(t *testing.T) {
	t.Parallel()

	store := NewStore(10, time.Minute)
	store.Start(100)
	store.Finish(Result{Status: 503, ResponseBytes: 30, Duration: time.Millisecond, Err: errors.New("failed")})

	stats := store.Snapshot()
	if stats.TotalRequests != 1 {
		t.Fatalf("total requests = %d, want 1", stats.TotalRequests)
	}
	if stats.TotalErrors != 1 {
		t.Fatalf("total errors = %d, want 1", stats.TotalErrors)
	}
	if stats.BytesIn != 100 {
		t.Fatalf("bytes in = %d, want 100", stats.BytesIn)
	}
	if stats.BytesOut != 30 {
		t.Fatalf("bytes out = %d, want 30", stats.BytesOut)
	}
	if stats.InFlight != 0 {
		t.Fatalf("in-flight = %d, want 0", stats.InFlight)
	}
}

func TestStoreSubscribe(t *testing.T) {
	t.Parallel()

	store := NewStore(10, time.Minute)
	ch, unsubscribe := store.Subscribe(1)
	t.Cleanup(unsubscribe)

	store.Start(0)
	store.Finish(Result{Status: 200, Duration: time.Millisecond})

	select {
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected event from subscription")
	case ev := <-ch:
		if ev.Status != 200 {
			t.Fatalf("event status=%d, want 200", ev.Status)
		}
	}
}
