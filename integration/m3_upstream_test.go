package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/rules"
	"github.com/fanzy618/pop/internal/upstream"
)

func TestRuleRoutesToDifferentUpstreams(t *testing.T) {
	t.Parallel()

	var hitsA atomic.Int64
	var hitsB atomic.Int64

	upstreamA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		_, _ = w.Write([]byte("from-upstream-a"))
	}))
	t.Cleanup(upstreamA.Close)

	upstreamB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		_, _ = w.Write([]byte("from-upstream-b"))
	}))
	t.Cleanup(upstreamB.Close)

	mgr, err := upstream.NewManager([]upstream.Config{
		{ID: "A", URL: upstreamA.URL, Enabled: true},
		{ID: "B", URL: upstreamB.URL, Enabled: true},
	})
	if err != nil {
		t.Fatalf("create upstream manager: %v", err)
	}

	matcher := rules.NewMatcher([]rules.Rule{
		{ID: "route-a", Enabled: true, Pattern: "alpha.pop.test", Action: rules.ActionProxy, UpstreamID: "A"},
		{ID: "route-b", Enabled: true, Pattern: "beta.pop.test", Action: rules.ActionProxy, UpstreamID: "B"},
	}, rules.Decision{Action: rules.ActionDirect})

	pop := httptest.NewServer(proxy.NewServerWithDeps(matcher, mgr))
	t.Cleanup(pop.Close)

	proxyURL, err := url.Parse(pop.URL)
	if err != nil {
		t.Fatalf("parse pop url: %v", err)
	}

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}

	if body := getBodyViaProxy(t, client, "http://alpha.pop.test/hello"); body != "from-upstream-a" {
		t.Fatalf("alpha response body = %q", body)
	}
	if body := getBodyViaProxy(t, client, "http://beta.pop.test/world"); body != "from-upstream-b" {
		t.Fatalf("beta response body = %q", body)
	}

	if hitsA.Load() != 1 {
		t.Fatalf("upstream A hits = %d, want 1", hitsA.Load())
	}
	if hitsB.Load() != 1 {
		t.Fatalf("upstream B hits = %d, want 1", hitsB.Load())
	}
}

func getBodyViaProxy(t *testing.T, client *http.Client, rawURL string) string {
	t.Helper()

	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body for %s: %v", rawURL, err)
	}

	return string(body)
}
