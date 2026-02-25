package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fanzy618/pop/internal/proxy"
)

func TestDirectHTTPProxyRequest(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-POP-Target", "ok")
		_, _ = w.Write([]byte("hello from target"))
	}))
	t.Cleanup(target.Close)

	proxyServer := httptest.NewServer(proxy.NewServer())
	t.Cleanup(proxyServer.Close)

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}

	resp, err := client.Get(target.URL + "/ping")
	if err != nil {
		t.Fatalf("proxy GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := resp.Header.Get("X-POP-Target"); got != "ok" {
		t.Fatalf("unexpected header X-POP-Target = %q", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "hello from target" {
		t.Fatalf("body = %q, want %q", string(body), "hello from target")
	}
}
