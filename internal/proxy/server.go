package proxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fanzy618/pop/internal/rules"
	"github.com/fanzy618/pop/internal/telemetry"
	"github.com/fanzy618/pop/internal/upstream"
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

// RouteSnapshot holds the immutable pair of (matcher, upstream-manager) that
// the data path uses to dispatch a single request. Publishing a new snapshot
// is the only way to mutate routing — fields are never modified in place.
type RouteSnapshot struct {
	Matcher   *rules.Matcher
	Upstreams *upstream.Manager
}

// NewSnapshot fills in safe defaults for nil arguments so callers can publish
// a partial snapshot without nil-checks.
func NewSnapshot(matcher *rules.Matcher, upstreams *upstream.Manager) *RouteSnapshot {
	if matcher == nil {
		matcher = rules.NewMatcher(nil, rules.Decision{Action: rules.ActionDirect})
	}
	if upstreams == nil {
		upstreams, _ = upstream.NewManager(nil)
	}
	return &RouteSnapshot{Matcher: matcher, Upstreams: upstreams}
}

type Server struct {
	directTransport *http.Transport
	dialer          *net.Dialer
	loopID          string

	snapshot    atomic.Pointer[RouteSnapshot]
	telemetry   atomic.Pointer[telemetry.Store]
	connections atomic.Pointer[telemetry.Connections]
}

func generateLoopID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewServerWithSnapshot constructs a Server pre-populated with snap. Useful
// for tests that want to wire in routing state at construction time.
func NewServerWithSnapshot(snap *RouteSnapshot) *Server {
	s := NewServer()
	s.Publish(snap)
	return s
}

// NewServer creates a Server with an empty default snapshot (DIRECT for all
// hosts) and a default in-memory telemetry store. Callers wire production
// state in via Publish / SetTelemetry.
func NewServer() *Server {
	s := &Server{
		directTransport: &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          128,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
		},
		dialer: &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
		loopID: generateLoopID(),
	}
	s.snapshot.Store(NewSnapshot(nil, nil))
	s.telemetry.Store(telemetry.NewStore(10000, 30*time.Minute))
	s.connections.Store(telemetry.NewConnections(0))
	return s
}

// Publish atomically swaps in a new routing snapshot. Concurrent requests
// observe either the old or new snapshot — never a torn pair.
func (s *Server) Publish(snap *RouteSnapshot) {
	if snap == nil {
		snap = NewSnapshot(nil, nil)
	}
	if snap.Matcher == nil || snap.Upstreams == nil {
		snap = NewSnapshot(snap.Matcher, snap.Upstreams)
	}
	s.snapshot.Store(snap)
}

// Snapshot returns the current routing snapshot. Read-only.
func (s *Server) Snapshot() *RouteSnapshot {
	return s.snapshot.Load()
}

// SetTelemetry swaps the telemetry sink. Hot-swappable for tests and for
// console wiring after construction.
func (s *Server) SetTelemetry(store *telemetry.Store) {
	if store == nil {
		store = telemetry.NewStore(10000, 30*time.Minute)
	}
	s.telemetry.Store(store)
}

// SetConnections swaps the connection registry. Used by main.go to share
// the registry with the console handler.
func (s *Server) SetConnections(c *telemetry.Connections) {
	if c == nil {
		c = telemetry.NewConnections(0)
	}
	s.connections.Store(c)
}

// Connections returns the current connection registry. Always non-nil.
func (s *Server) Connections() *telemetry.Connections {
	return s.connections.Load()
}

func (s *Server) decide(host string) rules.Decision {
	snap := s.snapshot.Load()
	if snap == nil || snap.Matcher == nil {
		return rules.Decision{Action: rules.ActionDirect}
	}
	return snap.Matcher.Decide(host)
}

func (s *Server) getUpstream(id string) (*upstream.Target, bool) {
	snap := s.snapshot.Load()
	if snap == nil || snap.Upstreams == nil {
		return nil, false
	}
	return snap.Upstreams.Get(id)
}

func (s *Server) getTelemetry() *telemetry.Store {
	return s.telemetry.Load()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Pop-Loop-Id") == s.loopID {
		http.Error(w, "Loop Detected", http.StatusLoopDetected)
		return
	}

	host := matchHost(r)
	decision := s.decide(host)
	tel := s.getTelemetry()

	result := telemetry.Result{
		Client:       r.RemoteAddr,
		Method:       r.Method,
		Host:         host,
		Action:       string(decision.Action),
		RuleID:       decision.RuleID,
		RequestBytes: maxInt64(r.ContentLength, 0),
	}

	// Register an in-flight entry so /api/connections can show it. May be
	// nil if the registry is at cap or absent; downstream handlers tolerate
	// nil byte counters.
	conn := s.openConnection(r, decision)
	var liveIn, liveOut *atomic.Int64
	if conn != nil {
		liveIn = &conn.BytesIn
		liveOut = &conn.BytesOut
		defer s.connections.Load().Close(conn.ID)
	}

	startAt := time.Now()
	if tel != nil {
		tel.Start(result.RequestBytes)
		defer func() {
			result.Duration = time.Since(startAt)
			tel.Finish(result)
		}()
	}

	if decision.Action == rules.ActionBlock {
		code := decision.BlockStatus
		if code == 0 {
			code = http.StatusNotFound
		}
		http.Error(w, http.StatusText(code), code)
		result.Status = code
		return
	}

	if decision.Action == rules.ActionProxy {
		if decision.UpstreamID == "" {
			http.Error(w, "upstream proxy id is required", http.StatusBadGateway)
			result.Status = http.StatusBadGateway
			result.Err = errors.New("upstream proxy id is required")
			return
		}

		target, ok := s.getUpstream(decision.UpstreamID)
		if !ok {
			http.Error(w, "upstream proxy is not available", http.StatusBadGateway)
			result.Status = http.StatusBadGateway
			result.Err = errors.New("upstream proxy is not available")
			return
		}

		if r.Method == http.MethodConnect {
			status, err := s.handleConnectViaUpstream(w, r, target, liveIn, liveOut)
			result.Status = status
			result.Err = err
			return
		}

		rw := newResponseRecorderLive(w, liveOut)
		s.handleHTTP(rw, r, target.Transport, liveIn)
		result.Status = rw.status
		result.ResponseBytes = rw.bytes
		return
	}

	if r.Method == http.MethodConnect {
		status, err := s.handleConnect(w, r, liveIn, liveOut)
		result.Status = status
		result.Err = err
		return
	}

	rw := newResponseRecorderLive(w, liveOut)
	s.handleHTTP(rw, r, s.directTransport, liveIn)
	result.Status = rw.status
	result.ResponseBytes = rw.bytes
}

// openConnection inserts an entry into the registry for the in-flight
// request. Returns nil when the registry is full or absent — callers handle
// nil byte counters gracefully.
func (s *Server) openConnection(r *http.Request, decision rules.Decision) *telemetry.ConnState {
	reg := s.connections.Load()
	if reg == nil {
		return nil
	}
	return reg.Open(telemetry.ConnState{
		Client:     r.RemoteAddr,
		Method:     r.Method,
		Host:       matchHost(r),
		Action:     string(decision.Action),
		RuleID:     decision.RuleID,
		UpstreamID: decision.UpstreamID,
	})
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request, transport *http.Transport, liveIn *atomic.Int64) {
	if transport == nil {
		http.Error(w, "transport unavailable", http.StatusBadGateway)
		return
	}

	upReq := r.Clone(context.Background())
	upReq.RequestURI = ""
	if upReq.URL.Scheme == "" {
		upReq.URL.Scheme = "http"
	}
	if upReq.URL.Host == "" {
		upReq.URL.Host = r.Host
	}
	removeHopByHopHeaders(upReq.Header)
	if s.loopID != "" {
		upReq.Header.Set("X-Pop-Loop-Id", s.loopID)
	}
	if liveIn != nil && upReq.Body != nil && upReq.Body != http.NoBody {
		upReq.Body = newCountedReader(upReq.Body, liveIn)
	}

	resp, err := transport.RoundTrip(upReq)
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

func (s *Server) handleConnectViaUpstream(w http.ResponseWriter, r *http.Request, target *upstream.Target, liveIn, liveOut *atomic.Int64) (int, error) {
	if target == nil || target.URL == nil {
		http.Error(w, "upstream proxy is invalid", http.StatusBadGateway)
		return http.StatusBadGateway, errors.New("upstream proxy is invalid")
	}

	connectTarget := normalizeHost(r.Host)
	if connectTarget == "" {
		http.Error(w, "bad connect target", http.StatusBadRequest)
		return http.StatusBadRequest, errors.New("bad connect target")
	}

	upstreamAddr := target.URL.Host
	if _, _, err := net.SplitHostPort(upstreamAddr); err != nil {
		upstreamAddr = net.JoinHostPort(upstreamAddr, "80")
	}

	upstreamConn, err := s.dialer.DialContext(r.Context(), "tcp", upstreamAddr)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway, err
	}

	if err := writeUpstreamConnectRequest(upstreamConn, connectTarget, target.URL, s.loopID); err != nil {
		upstreamConn.Close()
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway, err
	}

	br := bufio.NewReader(upstreamConn)
	upResp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		upstreamConn.Close()
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway, err
	}
	defer upResp.Body.Close()

	if upResp.StatusCode != http.StatusOK {
		upstreamConn.Close()
		http.Error(w, fmt.Sprintf("upstream connect failed: %d", upResp.StatusCode), http.StatusBadGateway)
		return http.StatusBadGateway, errors.New("upstream connect failed")
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		upstreamConn.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return http.StatusInternalServerError, errors.New("hijacking not supported")
	}

	clientConn, rw, err := hj.Hijack()
	if err != nil {
		upstreamConn.Close()
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return http.StatusInternalServerError, err
	}

	_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := rw.Flush(); err != nil {
		clientConn.Close()
		upstreamConn.Close()
		return http.StatusInternalServerError, err
	}

	tunnelWithCounters(clientConn, upstreamConn, liveIn, liveOut)
	return http.StatusOK, nil
}

func writeUpstreamConnectRequest(conn net.Conn, target string, upstreamURL *url.URL, loopID string) error {
	var b strings.Builder
	b.WriteString("CONNECT ")
	b.WriteString(target)
	b.WriteString(" HTTP/1.1\r\nHost: ")
	b.WriteString(target)
	b.WriteString("\r\n")

	if loopID != "" {
		b.WriteString("X-Pop-Loop-Id: ")
		b.WriteString(loopID)
		b.WriteString("\r\n")
	}

	if upstreamURL != nil && upstreamURL.User != nil {
		username := upstreamURL.User.Username()
		password, _ := upstreamURL.User.Password()
		raw := username + ":" + password
		authValue := base64.StdEncoding.EncodeToString([]byte(raw))
		b.WriteString("Proxy-Authorization: Basic ")
		b.WriteString(authValue)
		b.WriteString("\r\n")
	}

	b.WriteString("\r\n")
	_, err := conn.Write([]byte(b.String()))
	return err
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, liveIn, liveOut *atomic.Int64) (int, error) {
	target := normalizeHost(r.Host)
	if target == "" {
		http.Error(w, "bad connect target", http.StatusBadRequest)
		return http.StatusBadRequest, errors.New("bad connect target")
	}

	upConn, err := s.dialer.DialContext(r.Context(), "tcp", target)
	if err != nil {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return http.StatusBadGateway, err
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		upConn.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return http.StatusInternalServerError, errors.New("hijacking not supported")
	}

	clientConn, rw, err := hj.Hijack()
	if err != nil {
		upConn.Close()
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return http.StatusInternalServerError, err
	}

	_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := rw.Flush(); err != nil {
		clientConn.Close()
		upConn.Close()
		return http.StatusInternalServerError, err
	}

	tunnelWithCounters(clientConn, upConn, liveIn, liveOut)
	return http.StatusOK, nil
}

// tunnelWithCounters is tunnel + per-direction byte accounting via wrapping
// the client-side conn in countedConn.
func tunnelWithCounters(client, server net.Conn, liveIn, liveOut *atomic.Int64) {
	if liveIn != nil || liveOut != nil {
		client = wrapClientConn(client, liveIn, liveOut)
	}
	tunnel(client, server)
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

type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
	live        *atomic.Int64 // optional: mirrors bytes for live "connections" view
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

// newResponseRecorderLive is like newResponseRecorder but also reports each
// Write into live, so the in-flight connection view sees bytes-out grow.
func newResponseRecorderLive(w http.ResponseWriter, live *atomic.Int64) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK, live: live}
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	if !r.wroteHeader {
		r.status = statusCode
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)
	if n > 0 && r.live != nil {
		r.live.Add(int64(n))
	}
	return n, err
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
