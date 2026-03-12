package integration

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fanzy618/pop/internal/proxy"
)

func TestDirectProxy_Connect(t *testing.T) {
	t.Parallel()

	// 1. Start a TLS target server
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secure hello"))
	}))
	t.Cleanup(target.Close)

	// 2. Start POP proxy
	proxySrv := proxy.NewServer()
	proxyHttpSrv := httptest.NewServer(proxySrv)
	t.Cleanup(proxyHttpSrv.Close)

	proxyURL, _ := url.Parse(proxyHttpSrv.URL)

	// 3. Client using POP as proxy
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Target uses self-signed cert
			},
		},
	}

	resp, err := client.Get(target.URL)
	if err != nil {
		t.Fatalf("HTTPS GET via proxy failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "secure hello" {
		t.Errorf("body = %q, want %q", string(body), "secure hello")
	}
}

func TestDirectProxy_HopByHopHeaders(t *testing.T) {
	t.Parallel()

	// 1. Target server that checks for hop-by-hop headers
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify hop-by-hop headers from client are removed
		hops := []string{"Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"}
		for _, h := range hops {
			if r.Header.Get(h) != "" {
				t.Errorf("Target received hop-by-hop header: %s", h)
			}
		}

		// Set some hop-by-hop headers in response
		w.Header().Set("Proxy-Authenticate", "Basic realm=\"resticted\"")
		w.Header().Set("X-Normal-Header", "keep-me")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(target.Close)

	// 2. Start POP proxy
	proxySrv := proxy.NewServer()
	proxyHttpSrv := httptest.NewServer(proxySrv)
	t.Cleanup(proxyHttpSrv.Close)

	// 3. Send raw request with hop-by-hop headers to proxy
	proxyAddr := proxyHttpSrv.Listener.Addr().String()
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	// Construct raw HTTP request with absolute URL and hop-by-hop headers
	reqStr := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Proxy-Authenticate: something\r\n"+
		"Keep-Alive: timeout=5, max=100\r\n"+
		"X-Normal-Header: keep-me\r\n"+
		"Connection: close\r\n\r\n", target.URL, target.Listener.Addr().String())

	_, err = conn.Write([]byte(reqStr))
	if err != nil {
		t.Fatalf("write request: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()

	// 4. Verify hop-by-hop headers from target are removed by proxy
	if resp.Header.Get("Proxy-Authenticate") != "" {
		t.Errorf("Client received hop-by-hop header: Proxy-Authenticate")
	}
	if resp.Header.Get("X-Normal-Header") != "keep-me" {
		t.Errorf("Client lost normal header: X-Normal-Header")
	}
}

func TestDirectProxy_AbsoluteURL(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("absolute ok"))
	}))
	t.Cleanup(target.Close)

	proxySrv := proxy.NewServer()
	proxyHttpSrv := httptest.NewServer(proxySrv)
	t.Cleanup(proxyHttpSrv.Close)

	proxyAddr := proxyHttpSrv.Listener.Addr().String()
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	// HTTP proxies MUST support absolute URLs in request line
	reqStr := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\n\r\n", target.URL, target.Listener.Addr().String())
	_, _ = conn.Write([]byte(reqStr))

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "absolute ok" {
		t.Errorf("got body %q, want %q", string(body), "absolute ok")
	}
}

func TestDirectProxy_BadGateway(t *testing.T) {
	t.Parallel()

	proxySrv := proxy.NewServer()
	proxyHttpSrv := httptest.NewServer(proxySrv)
	t.Cleanup(proxyHttpSrv.Close)

	proxyURL, _ := url.Parse(proxyHttpSrv.URL)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}

	// Request a non-existent host
	resp, err := client.Get("http://nonexistent.local.invalid")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	// To check exact status code, we can use raw connection
	proxyAddr := proxyHttpSrv.Listener.Addr().String()
	conn, _ := net.Dial("tcp", proxyAddr)
	defer conn.Close()

	_, _ = conn.Write([]byte("GET http://nonexistent.local.invalid HTTP/1.1\r\nHost: nonexistent.local.invalid\r\n\r\n"))
	resp, err = http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (Bad Gateway)", resp.StatusCode, http.StatusBadGateway)
	}
}

func TestDirectProxy_ProxyConnectionHeader(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Connection") != "" {
			t.Errorf("Target received Proxy-Connection header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	proxySrv := proxy.NewServer()
	proxyHttpSrv := httptest.NewServer(proxySrv)
	t.Cleanup(proxyHttpSrv.Close)

	proxyAddr := proxyHttpSrv.Listener.Addr().String()
	conn, _ := net.Dial("tcp", proxyAddr)
	defer conn.Close()

	// Some browsers use Proxy-Connection instead of Connection
	reqStr := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nProxy-Connection: keep-alive\r\n\r\n", target.URL, target.Listener.Addr().String())
	_, _ = conn.Write([]byte(reqStr))

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}

	// Proxy should also remove Proxy-Connection from response if it were present, 
	// though standard HTTP servers don't usually send it.
	if resp.Header.Get("Proxy-Connection") != "" {
		t.Errorf("Client received Proxy-Connection header")
	}
}
