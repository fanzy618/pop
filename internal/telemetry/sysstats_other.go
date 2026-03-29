//go:build !linux

package telemetry

import (
	"time"
)

func readCPU() cpuSample {
	return cpuSample{sampled: time.Now()}
}
