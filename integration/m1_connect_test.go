package integration

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fanzy618/pop/internal/proxy"
)

func TestConnectTunnel(t *testing.T) {
	t.Parallel()

	target := newTCPHTTPServer(t)
	defer target.close()

	proxyServer := httptest.NewServer(proxy.NewServer())
	t.Cleanup(proxyServer.Close)

	proxyAddr := strings.TrimPrefix(proxyServer.URL, "http://")
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	_, _ = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target.addr, target.addr)
	br := bufio.NewReader(conn)

	connectResp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	defer connectResp.Body.Close()

	if connectResp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want %d", connectResp.StatusCode, http.StatusOK)
	}

	_, _ = fmt.Fprintf(conn, "GET /hello HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", target.addr)
	httpResp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("read tunneled response: %v", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", httpResp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "hello from tunnel" {
		t.Fatalf("body = %q, want %q", string(body), "hello from tunnel")
	}
}

type tcpHTTPServer struct {
	addr string
	lsn  net.Listener
}

func newTCPHTTPServer(t *testing.T) *tcpHTTPServer {
	t.Helper()

	lsn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/hello" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte("hello from tunnel"))
		}),
	}

	go func() {
		_ = server.Serve(lsn)
	}()

	t.Cleanup(func() {
		_ = server.Close()
	})

	return &tcpHTTPServer{addr: lsn.Addr().String(), lsn: lsn}
}

func (s *tcpHTTPServer) close() {
	_ = s.lsn.Close()
}
