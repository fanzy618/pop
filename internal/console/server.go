package console

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/telemetry"
	"github.com/fanzy618/pop/internal/upstream"
)

//go:embed assets/*
var staticAssets embed.FS

type Server struct {
	configPath string
	proxy      *proxy.Server
	telemetry  *telemetry.Store

	mu  sync.RWMutex
	cfg *config.Config

	username string
	password string

	mux http.Handler
}

func NewServer(cfg *config.Config, configPath string, proxyServer *proxy.Server, telemetryStore *telemetry.Store) (*Server, error) {
	if cfg == nil {
		cfg = config.Default()
	}
	if proxyServer == nil {
		return nil, errors.New("proxy server is required")
	}
	if telemetryStore == nil {
		return nil, errors.New("telemetry store is required")
	}

	s := &Server{
		configPath: configPath,
		proxy:      proxyServer,
		telemetry:  telemetryStore,
		cfg:        cloneConfig(cfg),
		username:   cfg.Auth.Username,
		password:   cfg.Auth.Password,
	}

	if err := s.applyConfigLocked(cloneConfig(cfg)); err != nil {
		return nil, err
	}
	assetsFS, err := fs.Sub(staticAssets, "assets")
	if err != nil {
		return nil, fmt.Errorf("load static assets: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS))))
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/upstreams", s.handleUpstreams)
	mux.HandleFunc("/api/upstreams/", s.handleUpstreamByID)
	mux.HandleFunc("/api/rules", s.handleRules)
	mux.HandleFunc("/api/rules/", s.handleRuleByID)
	mux.HandleFunc("/api/rules/reorder", s.handleRuleReorder)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/activities", s.handleActivities)
	mux.HandleFunc("/api/activities/stream", s.handleActivitiesStream)

	s.mux = s.authMiddleware(mux)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.username || pass != s.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="pop-console"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data, err := staticAssets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "console page is unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		cfg := cloneConfig(s.cfg)
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var next config.Config
		if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := s.updateConfig(&next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUpstreams(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		items := append([]config.UpstreamConfig(nil), s.cfg.Upstreams...)
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var up config.UpstreamConfig
		if err := json.NewDecoder(r.Body).Decode(&up); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if up.ID == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}

		s.mu.RLock()
		next := cloneConfig(s.cfg)
		s.mu.RUnlock()
		for _, current := range next.Upstreams {
			if current.ID == up.ID {
				http.Error(w, "upstream id already exists", http.StatusConflict)
				return
			}
		}
		next.Upstreams = append(next.Upstreams, up)
		if err := s.updateConfig(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, up)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUpstreamByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/upstreams/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	s.mu.RLock()
	next := cloneConfig(s.cfg)
	s.mu.RUnlock()

	idx := -1
	for i := range next.Upstreams {
		if next.Upstreams[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var up config.UpstreamConfig
		if err := json.NewDecoder(r.Body).Decode(&up); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		up.ID = id
		next.Upstreams[idx] = up
		if err := s.updateConfig(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, up)
	case http.MethodDelete:
		next.Upstreams = append(next.Upstreams[:idx], next.Upstreams[idx+1:]...)
		if err := s.updateConfig(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		items := append([]config.RuleConfig(nil), s.cfg.Rules...)
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var rule config.RuleConfig
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if rule.ID == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}

		s.mu.RLock()
		next := cloneConfig(s.cfg)
		s.mu.RUnlock()
		for _, current := range next.Rules {
			if current.ID == rule.ID {
				http.Error(w, "rule id already exists", http.StatusConflict)
				return
			}
		}
		next.Rules = append(next.Rules, rule)
		if err := s.updateConfig(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, rule)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRuleByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/rules/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	s.mu.RLock()
	next := cloneConfig(s.cfg)
	s.mu.RUnlock()

	idx := -1
	for i := range next.Rules {
		if next.Rules[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var rule config.RuleConfig
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		rule.ID = id
		next.Rules[idx] = rule
		if err := s.updateConfig(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, rule)
	case http.MethodDelete:
		next.Rules = append(next.Rules[:idx], next.Rules[idx+1:]...)
		if err := s.updateConfig(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type reorderRequest struct {
	IDs []string `json:"ids"`
}

func (s *Server) handleRuleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req reorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	next := cloneConfig(s.cfg)
	s.mu.RUnlock()

	index := make(map[string]int, len(req.IDs))
	for i, id := range req.IDs {
		index[id] = i + 1
	}
	for i := range next.Rules {
		if order, ok := index[next.Rules[i].ID]; ok {
			next.Rules[i].Order = order
		}
	}

	if err := s.updateConfig(next); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.telemetry.Snapshot())
}

func (s *Server) handleActivities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	writeJSON(w, http.StatusOK, s.telemetry.Recent(limit))
}

func (s *Server) handleActivitiesStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := s.telemetry.Subscribe(64)
	defer unsubscribe()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (s *Server) updateConfig(next *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyConfigLocked(next)
}

func (s *Server) applyConfigLocked(next *config.Config) error {
	next = cloneConfig(next)
	if err := next.Validate(); err != nil {
		return err
	}

	mgr, err := upstream.NewManager(next.BuildUpstreamConfigs())
	if err != nil {
		return err
	}

	s.proxy.SetMatcher(next.BuildMatcher())
	s.proxy.SetUpstreams(mgr)
	s.proxy.SetTelemetry(s.telemetry)

	if s.configPath != "" {
		if err := config.Save(s.configPath, next); err != nil {
			return err
		}
	}

	s.cfg = next
	s.username = next.Auth.Username
	s.password = next.Auth.Password
	return nil
}

func cloneConfig(in *config.Config) *config.Config {
	if in == nil {
		return config.Default()
	}
	raw, _ := json.Marshal(in)
	out := config.Default()
	_ = json.Unmarshal(raw, out)
	return out
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
