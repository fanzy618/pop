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

// A subscriber whose channel is full must not be able to block the data
// path: Finish() drops the event via select-default and returns promptly.
func TestStoreSubscribe_SlowConsumerDropsWithoutBlocking(t *testing.T) {
	t.Parallel()

	store := NewStore(1000, time.Minute)
	_, unsubscribe := store.Subscribe(1) // buffer=1, never read from
	t.Cleanup(unsubscribe)

	const bursts = 200
	done := make(chan struct{})
	go func() {
		for i := 0; i < bursts; i++ {
			store.Start(0)
			store.Finish(Result{Status: 200, Duration: time.Millisecond})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Finish blocked on slow subscriber")
	}

	// And the subscriber slot is still registered (no goroutine leak / close
	// of the channel) — second unsubscribe call must be a no-op.
	unsubscribe()
}

// Unsubscribe closes the channel; calling it twice is safe.
func TestStoreSubscribe_UnsubscribeIdempotent(t *testing.T) {
	t.Parallel()

	store := NewStore(10, time.Minute)
	ch, unsubscribe := store.Subscribe(4)

	unsubscribe()
	unsubscribe() // must not panic

	// Channel must be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected channel closed after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("channel not closed after unsubscribe")
	}
}

// Concurrent Start/Finish from many goroutines must end with InFlight==0 and
// no race detector hits.
func TestStoreCountersConcurrentStartFinish(t *testing.T) {
	t.Parallel()

	store := NewStore(10000, time.Minute)
	const goroutines = 50
	const perG = 100

	done := make(chan struct{}, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < perG; i++ {
				store.Start(1)
				store.Finish(Result{Status: 200, ResponseBytes: 1, Duration: time.Microsecond})
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < goroutines; g++ {
		<-done
	}

	stats := store.Snapshot()
	if stats.InFlight != 0 {
		t.Fatalf("InFlight=%d, want 0", stats.InFlight)
	}
	want := int64(goroutines * perG)
	if stats.TotalRequests != want {
		t.Fatalf("TotalRequests=%d, want %d", stats.TotalRequests, want)
	}
	if stats.BytesIn != want || stats.BytesOut != want {
		t.Fatalf("byte counters off: in=%d out=%d want %d", stats.BytesIn, stats.BytesOut, want)
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
