package telemetry

import "sync"

// eventBus fans out events to subscribers. Slow subscribers are tolerated
// via select-default in publish — events are dropped rather than allowed to
// stall the data path. Unsubscribe is idempotent.
type eventBus struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func newEventBus() *eventBus {
	return &eventBus{subs: make(map[chan Event]struct{})}
}

func (b *eventBus) subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan Event, buffer)

	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
	}
	return ch, unsubscribe
}

func (b *eventBus) publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
