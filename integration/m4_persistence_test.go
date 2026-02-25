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
	"github.com/fanzy618/pop/internal/upstream"
)

func TestConfigPersistsAcrossRestart(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "pop.json")
	initial := config.Default()
	initial.Rules = []config.RuleConfig{
		{ID: "block-ads", Enabled: true, Order: 1, Pattern: "*ads*", Action: rules.ActionBlock, BlockStatus: http.StatusGone},
	}

	if err := config.Save(configPath, initial); err != nil {
		t.Fatalf("save config: %v", err)
	}

	check := func() {
		loaded, err := config.Load(configPath)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}

		mgr, err := upstream.NewManager(loaded.BuildUpstreamConfigs())
		if err != nil {
			t.Fatalf("build upstream manager: %v", err)
		}

		pop := httptest.NewServer(proxy.NewServerWithDeps(loaded.BuildMatcher(), mgr))
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

		if resp.StatusCode != http.StatusGone {
			t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusGone)
		}
	}

	check()
	check()
}
