package telemetry

import "sync/atomic"

// counters tracks the aggregate request/byte counters surfaced via Snapshot.
// All operations are lock-free.
type counters struct {
	inFlight      atomic.Int64
	totalRequests atomic.Int64
	totalErrors   atomic.Int64
	bytesIn       atomic.Int64
	bytesOut      atomic.Int64
}

func (c *counters) start(requestBytes int64) {
	c.inFlight.Add(1)
	c.totalRequests.Add(1)
	if requestBytes > 0 {
		c.bytesIn.Add(requestBytes)
	}
}

func (c *counters) finish(responseBytes int64, isError bool) {
	c.inFlight.Add(-1)
	if responseBytes > 0 {
		c.bytesOut.Add(responseBytes)
	}
	if isError {
		c.totalErrors.Add(1)
	}
}

func (c *counters) snapshot() Stats {
	return Stats{
		InFlight:      c.inFlight.Load(),
		TotalRequests: c.totalRequests.Load(),
		TotalErrors:   c.totalErrors.Load(),
		BytesIn:       c.bytesIn.Load(),
		BytesOut:      c.bytesOut.Load(),
	}
}
