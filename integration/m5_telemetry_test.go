package integration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/telemetry"
)

func TestTelemetryCollectsRequestStats(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("telemetry-ok"))
	}))
	t.Cleanup(target.Close)

	store := telemetry.NewStore(100, time.Minute)
	handler := proxy.NewServer()
	handler.SetTelemetry(store)

	pop := httptest.NewServer(handler)
	t.Cleanup(pop.Close)

	proxyURL, err := url.Parse(pop.URL)
	if err != nil {
		t.Fatalf("parse pop url: %v", err)
	}

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := client.Get(target.URL + "/a")
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	_ = resp.Body.Close()

	stats := store.Snapshot()
	if stats.TotalRequests != 1 {
		t.Fatalf("total requests = %d, want 1", stats.TotalRequests)
	}
	if stats.InFlight != 0 {
		t.Fatalf("in-flight = %d, want 0", stats.InFlight)
	}

	events := store.Recent(5)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Status != http.StatusOK {
		t.Fatalf("event status = %d, want %d", events[0].Status, http.StatusOK)
	}
	if events[0].Action != "DIRECT" {
		t.Fatalf("event action = %q, want %q", events[0].Action, "DIRECT")
	}
}
