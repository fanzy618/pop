package integration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/rules"
)

func TestRuleBlockStatusCode(t *testing.T) {
	t.Parallel()

	matcher := rules.NewMatcher([]rules.Rule{
		{ID: "block-ads", Enabled: true, Pattern: "*ads*", Action: rules.ActionBlock, BlockStatus: http.StatusGone},
	}, rules.Decision{Action: rules.ActionDirect})

	proxyServer := httptest.NewServer(proxy.NewServerWithMatcher(matcher))
	t.Cleanup(proxyServer.Close)

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := client.Get("http://ads-example.local/path")
	if err != nil {
		t.Fatalf("request through proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGone {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusGone)
	}
}
