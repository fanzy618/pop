package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/console"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/rules"
	"github.com/fanzy618/pop/internal/store"
	"github.com/fanzy618/pop/internal/telemetry"
)

func TestPACGeneration(t *testing.T) {
	t.Parallel()

	// 1. Setup dependencies
	db, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	// Add an upstream first
	_ = db.CreateUpstream(&config.UpstreamConfig{URL: "http://upstream:8080", Enabled: true})

	// Add some rules
	_ = db.CreateRule(&config.RuleConfig{Pattern: "google.com", Action: rules.ActionProxy, UpstreamID: 1, Enabled: true})
	_ = db.CreateRule(&config.RuleConfig{Pattern: "local.dev", Action: rules.ActionDirect, Enabled: true})
	_ = db.CreateRule(&config.RuleConfig{Pattern: "ads.com", Action: rules.ActionBlock, Enabled: true})

	proxySrv := proxy.NewServer()
	tel := telemetry.NewStore(10, 0)
	cfg := config.Default()
	cfg.ProxyListen = "0.0.0.0:5128"

	srv, err := console.NewServer(cfg, db, proxySrv, tel)
	if err != nil {
		t.Fatalf("new console server: %v", err)
	}

	// 2. Request PAC
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/proxy.pac")
	if err != nil {
		t.Fatalf("GET /proxy.pac failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := resp.Header.Get("Content-Type"); got != "application/x-ns-proxy-autoconfig" {
		t.Errorf("Content-Type = %q, want application/x-ns-proxy-autoconfig", got)
	}

	body, _ := io.ReadAll(resp.Body)
	pac := string(body)

	// 3. Verify content
	expectations := []string{
		"function FindProxyForURL(url, host)",
		"if (host === \"google.com\" || host.endsWith(\".google.com\")) return \"PROXY",
		"if (host === \"local.dev\" || host.endsWith(\".local.dev\")) return \"DIRECT\"",
		"if (host === \"ads.com\" || host.endsWith(\".ads.com\")) return \"PROXY 127.0.0.1:65535\"",
	}

	for _, exp := range expectations {
		if !strings.Contains(pac, exp) {
			t.Errorf("PAC missing expected content: %q\nFull PAC:\n%s", exp, pac)
		}
	}
}
