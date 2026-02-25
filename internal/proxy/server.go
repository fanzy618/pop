package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fanzy618/pop/internal/rules"
)

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

type Server struct {
	transport *http.Transport
	dialer    *net.Dialer

	mu      sync.RWMutex
	matcher *rules.Matcher
}

func NewServer() *Server {
	return NewServerWithMatcher(nil)
}

func NewServerWithMatcher(matcher *rules.Matcher) *Server {
	if matcher == nil {
		matcher = rules.NewMatcher(nil, rules.Decision{Action: rules.ActionDirect})
	}

	return &Server{
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          128,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
		},
		dialer:  &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
		matcher: matcher,
	}
}

func (s *Server) SetMatcher(matcher *rules.Matcher) {
	if matcher == nil {
		matcher = rules.NewMatcher(nil, rules.Decision{Action: rules.ActionDirect})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.matcher = matcher
}

func (s *Server) decide(host string) rules.Decision {
	s.mu.RLock()
	matcher := s.matcher
	s.mu.RUnlock()

	if matcher == nil {
		return rules.Decision{Action: rules.ActionDirect}
	}

	return matcher.Decide(host)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := matchHost(r)
	decision := s.decide(host)

	if decision.Action == rules.ActionBlock {
		code := decision.BlockStatus
		if code == 0 {
			code = http.StatusNotFound
		}
		http.Error(w, http.StatusText(code), code)
		return
	}

	if decision.Action == rules.ActionProxy {
		http.Error(w, "upstream proxy routing is not available yet", http.StatusBadGateway)
		return
	}

	if r.Method == http.MethodConnect {
		s.handleConnect(w, r)
		return
	}

	s.handleHTTP(w, r)
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	upReq := r.Clone(context.Background())
	upReq.RequestURI = ""
	if upReq.URL.Scheme == "" {
		upReq.URL.Scheme = "http"
	}
	if upReq.URL.Host == "" {
		upReq.URL.Host = r.Host
	}
	removeHopByHopHeaders(upReq.Header)

	resp, err := s.transport.RoundTrip(upReq)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	removeHopByHopHeaders(resp.Header)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	target := normalizeHost(r.Host)
	if target == "" {
		http.Error(w, "bad connect target", http.StatusBadRequest)
		return
	}

	upConn, err := s.dialer.DialContext(r.Context(), "tcp", target)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		upConn.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, rw, err := hj.Hijack()
	if err != nil {
		upConn.Close()
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return
	}

	_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := rw.Flush(); err != nil {
		clientConn.Close()
		upConn.Close()
		return
	}

	tunnel(clientConn, upConn)
}

func tunnel(a, b net.Conn) {
	defer a.Close()
	defer b.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		closeWrite(a)
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		closeWrite(b)
	}()

	wg.Wait()
}

func closeWrite(conn net.Conn) {
	type closeWriter interface {
		CloseWrite() error
	}
	if cw, ok := conn.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}

func removeHopByHopHeaders(h http.Header) {
	if h == nil {
		return
	}

	if connection := h.Get("Connection"); connection != "" {
		for _, token := range strings.Split(connection, ",") {
			h.Del(strings.TrimSpace(token))
		}
	}

	for _, key := range hopByHopHeaders {
		h.Del(key)
	}
}

func copyHeader(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

func normalizeHost(raw string) string {
	host := strings.TrimSpace(strings.ToLower(raw))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return ""
	}

	if strings.HasPrefix(host, "[") {
		parsedHost, port, err := net.SplitHostPort(host)
		if err == nil {
			if port == "" {
				return ""
			}
			return net.JoinHostPort(parsedHost, port)
		}
		if strings.Contains(err.Error(), "missing port in address") {
			return host
		}
		return ""
	}

	parsedHost, port, err := net.SplitHostPort(host)
	if err == nil {
		if parsedHost == "" || port == "" {
			return ""
		}
		return net.JoinHostPort(parsedHost, port)
	}

	var addrErr *net.AddrError
	if errors.As(err, &addrErr) {
		if strings.Contains(addrErr.Err, "missing port") {
			return host
		}
	}

	if strings.Count(host, ":") == 0 {
		return host
	}

	return ""
}

func requestHost(r *http.Request) string {
	if r == nil {
		return ""
	}

	if r.URL != nil && r.URL.Host != "" {
		return normalizeHost(r.URL.Host)
	}

	if r.Host != "" {
		return normalizeHost(r.Host)
	}

	return ""
}

func matchHost(r *http.Request) string {
	host := requestHost(r)
	if host == "" {
		return ""
	}

	if strings.HasPrefix(host, "[") {
		parsedHost, _, err := net.SplitHostPort(host)
		if err == nil {
			return strings.TrimSuffix(strings.ToLower(parsedHost), ".")
		}
		return strings.TrimSuffix(strings.ToLower(host), ".")
	}

	parsedHost, _, err := net.SplitHostPort(host)
	if err == nil {
		return strings.TrimSuffix(strings.ToLower(parsedHost), ".")
	}

	if strings.Count(host, ":") == 0 {
		return strings.TrimSuffix(strings.ToLower(host), ".")
	}

	return strings.TrimSuffix(strings.ToLower(host), ".")
}
