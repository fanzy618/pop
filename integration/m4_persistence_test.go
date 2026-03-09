package integration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/rules"
	"github.com/fanzy618/pop/internal/store"
	"github.com/fanzy618/pop/internal/upstream"
)

func TestConfigPersistsAcrossRestart(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "pop.sqlite")
	cfg := config.Default()
	cfg.SQLitePath = dbPath
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	db, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	rule := config.RuleConfig{Enabled: true, Pattern: "ads-pop.test", Action: rules.ActionBlock, BlockStatus: http.StatusGone}
	if err := db.CreateRule(&rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	check := func() {
		db, err := store.OpenSQLite(cfg.SQLitePath)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()
		upstreamItems, err := db.ListUpstreams()
		if err != nil {
			t.Fatalf("list upstreams: %v", err)
		}
		ruleItems, err := db.ListRules()
		if err != nil {
			t.Fatalf("list rules: %v", err)
		}

		mgr, err := upstream.NewManager(config.BuildUpstreamConfigs(upstreamItems))
		if err != nil {
			t.Fatalf("build upstream manager: %v", err)
		}

		pop := httptest.NewServer(proxy.NewServerWithDeps(cfg.BuildMatcher(ruleItems), mgr))
		defer pop.Close()

		proxyURL, err := url.Parse(pop.URL)
		if err != nil {
			t.Fatalf("parse proxy url: %v", err)
		}

		client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
		resp, err := client.Get("http://ads-pop.test/path")
		if err != nil {
			t.Fatalf("request through proxy: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusNotFound)
		}
	}

	check()
	check()
}
