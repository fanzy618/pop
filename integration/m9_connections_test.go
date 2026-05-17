package integration

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fanzy618/pop/internal/proxy"
)

// pacedReader emits one chunk every interval. Used by the connections
// integration test to make request-body byte counts observable: without
// pacing the kernel send buffer swallows a small body in one shot and
// the polling window misses the "in-flight, partial" state.
type pacedReader struct {
	chunk    []byte
	emitted  int
	chunks   int
	interval time.Duration
	done     bool
}

func (p *pacedReader) Read(b []byte) (int, error) {
	if p.done {
		return 0, io.EOF
	}
	if p.emitted > 0 {
		time.Sleep(p.interval)
	}
	n := copy(b, p.chunk)
	p.emitted++
	if p.emitted >= p.chunks {
		p.done = true
	}
	return n, nil
}

func (p *pacedReader) Close() error { return nil }

// During a streamed POST through the proxy, the connection registry must
// expose a row whose bytes_in grows mid-request, then disappear when the
// request completes.
func TestConnections_HTTPRequestBodyIsCountedLive(t *testing.T) {
	t.Parallel()

	// Pace the upload: 8 chunks × 30 ms = ~240 ms of in-flight time, so
	// polling every 15 ms will reliably observe partial bytes_in.
	const chunkSize = 32 * 1024
	const chunks = 8
	const wantSize = chunkSize * chunks
	chunk := make([]byte, chunkSize)
	for i := range chunk {
		chunk[i] = byte(i)
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)

	popProxy := proxy.NewServer()
	popHTTP := httptest.NewServer(popProxy)
	t.Cleanup(popHTTP.Close)

	popURL, _ := url.Parse(popHTTP.URL)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(popURL)},
		Timeout:   10 * time.Second,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		body := &pacedReader{chunk: chunk, chunks: chunks, interval: 30 * time.Millisecond}
		req, _ := http.NewRequest(http.MethodPost, target.URL+"/up", body)
		req.ContentLength = int64(wantSize)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()

	// Wait until the registry holds the in-flight entry with non-zero bytes_in.
	deadline := time.Now().Add(3 * time.Second)
	var sawProgress bool
	var lastLen int
	var lastBytesIn int64
	for time.Now().Before(deadline) {
		snap := popProxy.Connections().Snapshot()
		lastLen = len(snap)
		if len(snap) == 1 {
			lastBytesIn = snap[0].BytesIn
			if snap[0].BytesIn > 0 && snap[0].BytesIn < int64(wantSize) {
				sawProgress = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !sawProgress {
		t.Fatalf("never saw an in-flight entry with partial bytes_in (last len=%d bytesIn=%d want<%d)", lastLen, lastBytesIn, wantSize)
	}

	<-done

	if got := popProxy.Connections().Len(); got != 0 {
		t.Fatalf("registry not drained after request: len=%d", got)
	}
}

// CONNECT tunnel byte counting: open a tunnel, push N bytes both ways, and
// verify the registry sees the totals before the tunnel closes.
func TestConnections_CONNECTTunnelBytesAreCounted(t *testing.T) {
	t.Parallel()

	// A real HTTPS server is the easiest "tunnel destination" — we exchange
	// a TLS handshake + a short request/response with it. The handshake
	// alone moves a few KB in both directions, which is what we'll observe.
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := strings.Repeat("x", 4096)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(target.Close)

	popProxy := proxy.NewServer()
	popHTTP := httptest.NewServer(popProxy)
	t.Cleanup(popHTTP.Close)

	popURL, _ := url.Parse(popHTTP.URL)
	transport := &http.Transport{
		Proxy:           http.ProxyURL(popURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // httptest cert
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := client.Get(target.URL + "/")
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	// Wait until we see a CONNECT entry with non-zero bytes in either dir.
	deadline := time.Now().Add(3 * time.Second)
	var sawCounted bool
	for time.Now().Before(deadline) {
		snap := popProxy.Connections().Snapshot()
		for _, c := range snap {
			if c.Method == http.MethodConnect && (c.BytesIn > 0 || c.BytesOut > 0) {
				sawCounted = true
				break
			}
		}
		if sawCounted {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !sawCounted {
		t.Fatalf("CONNECT tunnel byte counters never advanced")
	}

	<-done

	// Close idle conns so the client tears down its CONNECT tunnel and the
	// proxy's ServeHTTP returns, firing the registry's Close.
	transport.CloseIdleConnections()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if popProxy.Connections().Len() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := popProxy.Connections().Len(); got != 0 {
		t.Fatalf("registry not drained: len=%d", got)
	}
}

// /api/connections returns a non-empty array for an in-flight request and
// an empty array once it ends.
func TestConnections_APIEndpointSnapshot(t *testing.T) {
	t.Parallel()

	consoleURL, proxyURL, _, client := setupConsoleAndProxy(t)

	// Slow target so we have time to poll /api/connections mid-request.
	begin := make(chan struct{})
	release := make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(begin)
		<-release
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)
	// Avoid panic on double-close in test cleanup if test exits early.
	t.Cleanup(func() { defer func() { _ = recover() }(); close(release) })

	proxyParsed, _ := url.Parse(proxyURL)
	proxyClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyParsed)}, Timeout: 5 * time.Second}

	var done atomic.Bool
	go func() {
		resp, err := proxyClient.Get(target.URL + "/slow")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		done.Store(true)
	}()

	select {
	case <-begin:
	case <-time.After(2 * time.Second):
		t.Fatalf("target never received the proxied request")
	}

	// Poll /api/connections — at least one row, with our host.
	deadline := time.Now().Add(2 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		resp, err := client.Get(consoleURL + "/api/connections")
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		text := string(body)
		host, _, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
		if strings.Contains(text, host) && strings.Contains(text, "\"action\":\"DIRECT\"") {
			found = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatalf("never saw in-flight entry in /api/connections")
	}

	close(release)
	// After completion, the registry must drain.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !done.Load() {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		resp, _ := client.Get(consoleURL + "/api/connections")
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if strings.TrimSpace(string(body)) == "[]" || strings.TrimSpace(string(body)) == "null" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("/api/connections did not drain after request finished")
}
