package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/console"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/store"
	"github.com/fanzy618/pop/internal/telemetry"
)

// /api/stats/history must return the samples produced by SysStatsCollector
// with all expected JSON fields populated.
func TestStatsHistory_ReturnsSamplesAndShape(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "pop.sqlite")
	cfg := config.Default()
	cfg.SQLitePath = dbPath

	proxyServer := proxy.NewServer()
	telStore := telemetry.NewStore(1000, time.Minute)
	proxyServer.SetTelemetry(telStore)
	db, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 50ms sampling interval, 1m window, capacity 1000.
	sysStats := telemetry.NewSysStatsCollector(telStore.Snapshot, 1000, 50*time.Millisecond, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sysStats.Start(ctx)

	consoleServer, err := console.NewServer(cfg, db, proxyServer, telStore, sysStats, proxyServer.Connections())
	if err != nil {
		t.Fatalf("new console server: %v", err)
	}
	consoleHTTP := httptest.NewServer(consoleServer)
	t.Cleanup(consoleHTTP.Close)

	// Wait long enough for at least 3 samples.
	time.Sleep(250 * time.Millisecond)

	resp, err := http.Get(consoleHTTP.URL + "/api/stats/history?window=1m")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=200", resp.StatusCode)
	}

	var samples []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&samples); err != nil {
		t.Fatalf("decode samples: %v", err)
	}
	if len(samples) < 2 {
		t.Fatalf("expected at least 2 samples, got %d", len(samples))
	}

	required := []string{
		"time", "cpu_percent", "memory_bytes", "memory_percent",
		"bytes_in", "bytes_out", "connections", "total_requests",
		"total_errors", "goroutines", "heap_alloc_bytes",
	}
	for _, k := range required {
		if _, ok := samples[0][k]; !ok {
			t.Fatalf("sample missing field %q: %+v", k, samples[0])
		}
	}

	// Samples should be in ascending time order.
	prev := ""
	for i, s := range samples {
		ts, _ := s["time"].(string)
		if ts == "" {
			t.Fatalf("sample %d has empty time", i)
		}
		if ts < prev {
			t.Fatalf("samples not in ascending order at index %d: %q < %q", i, ts, prev)
		}
		prev = ts
	}
}

// With no sysStats wired, the endpoint must still return 200 with an empty
// array — the UI consumes the body unconditionally.
func TestStatsHistory_EmptyWhenSysStatsAbsent(t *testing.T) {
	t.Parallel()

	consoleURL, _, _, client := setupConsoleAndProxy(t)
	resp, err := client.Get(consoleURL + "/api/stats/history")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=200", resp.StatusCode)
	}
	var samples []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&samples); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(samples) != 0 {
		t.Fatalf("expected empty array, got %d samples", len(samples))
	}
}
