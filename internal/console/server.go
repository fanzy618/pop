package console

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/fanzy618/pop/internal/config"
	"github.com/fanzy618/pop/internal/proxy"
	"github.com/fanzy618/pop/internal/store"
	"github.com/fanzy618/pop/internal/telemetry"
	"github.com/fanzy618/pop/internal/upstream"
)

//go:embed assets/*
var staticAssets embed.FS

type Server struct {
	db        *store.SQLite
	proxy     *proxy.Server
	telemetry *telemetry.Store

	mu  sync.RWMutex
	cfg *config.Config

	mux http.Handler
}

func NewServer(cfg *config.Config, db *store.SQLite, proxyServer *proxy.Server, telemetryStore *telemetry.Store) (*Server, error) {
	if cfg == nil {
		cfg = config.Default()
	}
	if db == nil {
		return nil, errors.New("sqlite store is required")
	}
	if proxyServer == nil {
		return nil, errors.New("proxy server is required")
	}
	if telemetryStore == nil {
		return nil, errors.New("telemetry store is required")
	}

	s := &Server{
		db:        db,
		proxy:     proxyServer,
		telemetry: telemetryStore,
		cfg:       cloneConfig(cfg),
	}

	if err := s.applyBaseConfigLocked(cloneConfig(cfg)); err != nil {
		return nil, err
	}
	if err := s.rebuildRuntimeLocked(); err != nil {
		return nil, err
	}
	assetsFS, err := fs.Sub(staticAssets, "assets")
	if err != nil {
		return nil, fmt.Errorf("load static assets: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS))))
	mux.HandleFunc("/", s.handlePage)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/upstreams", s.handleUpstreams)
	mux.HandleFunc("/api/upstreams/", s.handleUpstreamByID)
	mux.HandleFunc("/api/rules", s.handleRules)
	mux.HandleFunc("/api/rules/", s.handleRuleByID)
	mux.HandleFunc("/api/rules/reorder", s.handleRuleReorder)
	mux.HandleFunc("/api/data/backup", s.handleDataBackup)
	mux.HandleFunc("/api/data/restore", s.handleDataRestore)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/activities", s.handleActivities)
	mux.HandleFunc("/api/activities/stream", s.handleActivitiesStream)

	s.mux = mux
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pages := map[string]string{
		"/":           "assets/index.html",
		"/stats":      "assets/index.html",
		"/activities": "assets/activities.html",
		"/rules":      "assets/rules.html",
		"/upstreams":  "assets/upstreams.html",
		"/data":       "assets/data.html",
	}

	assetPath, ok := pages[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}

	data, err := staticAssets.ReadFile(assetPath)
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
		upstreams, err := s.db.ListUpstreams()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rules, err := s.db.ListRules()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"proxy_listen":   cfg.ProxyListen,
			"console_listen": cfg.ConsoleListen,
			"sqlite_path":    cfg.SQLitePath,
			"default_action": cfg.DefaultAction,
			"upstreams":      upstreams,
			"rules":          rules,
		})
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
		items, err := s.db.ListUpstreams()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var up config.UpstreamConfig
		if err := json.NewDecoder(r.Body).Decode(&up); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		up.ID = 0
		up.Name = strings.TrimSpace(up.Name)
		up.URL = strings.TrimSpace(up.URL)
		if up.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		if err := s.db.CreateUpstream(&up); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.rebuildRuntime(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, up)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUpstreamByID(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimPrefix(r.URL.Path, "/api/upstreams/")
	id, ok := parseIntPath(rawID)
	if !ok {
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
		up.Name = strings.TrimSpace(up.Name)
		up.URL = strings.TrimSpace(up.URL)
		up.ID = id
		if err := s.db.UpdateUpstream(id, up); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.rebuildRuntime(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, up)
	case http.MethodDelete:
		if err := s.db.DeleteUpstream(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			if isForeignKeyConstraint(err) {
				http.Error(w, "upstream is referenced by rules", http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.rebuildRuntime(); err != nil {
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
		items, err := s.db.ListRules()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var rule config.RuleConfig
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		rule.ID = 0
		rule.Pattern = strings.TrimSpace(rule.Pattern)
		if rule.Pattern == "" {
			http.Error(w, "pattern is required", http.StatusBadRequest)
			return
		}
		if rule.Action == "BLOCK" {
			rule.BlockStatus = 404
		}
		if err := s.db.CreateRule(&rule); err != nil {
			if isForeignKeyConstraint(err) {
				http.Error(w, "unknown upstream_id", http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.rebuildRuntime(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, rule)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRuleByID(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimPrefix(r.URL.Path, "/api/rules/")
	id, ok := parseIntPath(rawID)
	if !ok {
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
		rule.Pattern = strings.TrimSpace(rule.Pattern)
		if rule.Pattern == "" {
			http.Error(w, "pattern is required", http.StatusBadRequest)
			return
		}
		if rule.Action == "BLOCK" {
			rule.BlockStatus = 404
		}
		rule.ID = id
		if err := s.db.UpdateRule(id, rule); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			if isForeignKeyConstraint(err) {
				http.Error(w, "unknown upstream_id", http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.rebuildRuntime(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, rule)
	case http.MethodDelete:
		if err := s.db.DeleteRule(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.rebuildRuntime(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type reorderRequest struct {
	IDs []int64 `json:"ids"`
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reorder_disabled": true})
}

func (s *Server) handleDataBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, err := s.db.ExportBackup()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleDataRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload store.BackupPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := s.db.RestoreBackup(&payload); err != nil {
		if strings.Contains(err.Error(), "unsupported data_format_version") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.rebuildRuntime(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "upstreams": len(payload.Upstreams), "rules": len(payload.Rules)})
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
	if err := s.applyBaseConfigLocked(next); err != nil {
		return err
	}
	return s.rebuildRuntimeLocked()
}

func (s *Server) rebuildRuntime() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rebuildRuntimeLocked()
}

func (s *Server) rebuildRuntimeLocked() error {
	upstreamItems, err := s.db.ListUpstreams()
	if err != nil {
		return err
	}
	ruleItems, err := s.db.ListRules()
	if err != nil {
		return err
	}
	if err := config.ValidateRuntime(upstreamItems, ruleItems); err != nil {
		return err
	}

	mgr, err := upstream.NewManager(config.BuildUpstreamConfigs(upstreamItems))
	if err != nil {
		return err
	}

	s.proxy.SetMatcher(s.cfg.BuildMatcher(ruleItems))
	s.proxy.SetUpstreams(mgr)
	s.proxy.SetTelemetry(s.telemetry)
	return nil
}

func (s *Server) applyBaseConfigLocked(next *config.Config) error {
	next = cloneConfig(next)
	if err := next.Validate(); err != nil {
		return err
	}

	s.cfg = next
	return nil
}

func parseIntPath(raw string) (int64, bool) {
	raw, err := url.PathUnescape(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func isForeignKeyConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key")
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
