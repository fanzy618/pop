package integration

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/rules"
	"github.com/fanzy618/pop/internal/upstream"
)

// CONNECT through an upstream that refuses the tunnel must surface as a 502
// to the client rather than hanging or panicking.
func TestProxy_ConnectViaUpstream_Non200ReturnsBadGateway(t *testing.T) {
	t.Parallel()

	// Fake upstream HTTP proxy that always rejects CONNECT with 403.
	fakeUpstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = fakeUpstream.Close() })

	go func() {
		for {
			c, err := fakeUpstream.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				_, _ = http.ReadRequest(br) // consume the CONNECT
				_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
			}(c)
		}
	}()

	mgr, err := upstream.NewManager([]upstream.Config{
		{ID: "deny", URL: "http://" + fakeUpstream.Addr().String(), Enabled: true},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	matcher := rules.NewMatcher([]rules.Rule{
		{ID: "r1", Enabled: true, Pattern: "secret.test", Action: rules.ActionProxy, UpstreamID: "deny"},
	}, rules.Decision{Action: rules.ActionDirect})

	pop := httptest.NewServer(proxy.NewServerWithSnapshot(proxy.NewSnapshot(matcher, mgr)))
	t.Cleanup(pop.Close)

	popURL, _ := url.Parse(pop.URL)
	tlsClient := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(popURL)},
		Timeout:   3 * time.Second,
	}

	// CONNECT path is triggered by https:// scheme.
	resp, err := tlsClient.Get("https://secret.test/")
	if err == nil {
		_ = resp.Body.Close()
		// Go's transport returns an error if the proxy refuses CONNECT —
		// the error message must mention 502 / Bad Gateway, not hang.
		t.Fatalf("expected error from CONNECT failure, got status=%d", resp.StatusCode)
	}
	if !strings.Contains(err.Error(), "502") && !strings.Contains(err.Error(), "Bad Gateway") {
		t.Fatalf("error does not mention 502/Bad Gateway: %v", err)
	}
}

// An upstream that accepts the TCP dial but then closes without responding
// must surface as a 502, not as a long hang or unhandled goroutine leak.
func TestProxy_UpstreamHTTPResponseTruncated_BadGateway(t *testing.T) {
	t.Parallel()

	// Fake upstream that accepts then immediately closes.
	upListen, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = upListen.Close() })
	go func() {
		for {
			c, err := upListen.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	mgr, err := upstream.NewManager([]upstream.Config{
		{ID: "bad", URL: "http://" + upListen.Addr().String(), Enabled: true},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	matcher := rules.NewMatcher([]rules.Rule{
		{ID: "r1", Enabled: true, Pattern: "truncated.test", Action: rules.ActionProxy, UpstreamID: "bad"},
	}, rules.Decision{Action: rules.ActionDirect})

	pop := httptest.NewServer(proxy.NewServerWithSnapshot(proxy.NewSnapshot(matcher, mgr)))
	t.Cleanup(pop.Close)

	popURL, _ := url.Parse(pop.URL)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(popURL)},
		Timeout:   3 * time.Second,
	}

	start := time.Now()
	resp, err := client.Get("http://truncated.test/x")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d, want=502", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("response took %v — proxy may not be failing fast", elapsed)
	}
}

// A PROXY rule pointing at an upstream id that does not exist must yield 502
// rather than panic or hang.
func TestProxy_UnknownUpstreamID_BadGateway(t *testing.T) {
	t.Parallel()

	mgr, _ := upstream.NewManager(nil)
	matcher := rules.NewMatcher([]rules.Rule{
		{ID: "r1", Enabled: true, Pattern: "ghost.test", Action: rules.ActionProxy, UpstreamID: "does-not-exist"},
	}, rules.Decision{Action: rules.ActionDirect})

	pop := httptest.NewServer(proxy.NewServerWithSnapshot(proxy.NewSnapshot(matcher, mgr)))
	t.Cleanup(pop.Close)

	popURL, _ := url.Parse(pop.URL)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(popURL)},
		Timeout:   3 * time.Second,
	}

	resp, err := client.Get("http://ghost.test/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d, want=502", resp.StatusCode)
	}
}
