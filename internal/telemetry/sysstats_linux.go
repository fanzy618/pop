//go:build linux

package telemetry

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func readCPU() cpuSample {
	s := cpuSample{sampled: time.Now()}
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return s
	}
	line := ""
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "cpu ") {
			line = l
			break
		}
	}
	if line == "" {
		return s
	}
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return s
	}
	s.user = parseU64(fields[1])
	s.nice = parseU64(fields[2])
	s.system = parseU64(fields[3])
	s.idle = parseU64(fields[4])
	if len(fields) >= 8 {
		s.stolen = parseU64(fields[7])
	}
	s.total = s.user + s.nice + s.system + s.idle + s.stolen
	return s
}

func parseU64(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}
