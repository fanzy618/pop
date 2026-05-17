package console

import (
	"net"
	"net/http"
)

func (s *Server) handlePAC(w http.ResponseWriter, r *http.Request) {
	snap := s.proxy.Snapshot()
	if snap == nil || snap.Matcher == nil {
		http.Error(w, "matcher not available", http.StatusInternalServerError)
		return
	}

	s.mu.RLock()
	proxyListen := s.cfg.ProxyListen
	pacOverride := s.cfg.PACProxyAddr
	s.mu.RUnlock()

	var proxyAddr string
	if pacOverride != "" {
		proxyAddr = pacOverride
	} else {
		host, _, _ := net.SplitHostPort(r.Host)
		if host == "" {
			host = r.Host
		}
		_, port, _ := net.SplitHostPort(proxyListen)
		if port == "" {
			port = "5128"
		}
		proxyAddr = net.JoinHostPort(host, port)
	}

	pac := snap.Matcher.GeneratePAC(proxyAddr)
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(pac))
}
