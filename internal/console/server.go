// Package console serves the web console: a small REST API for CRUD over
// rules and upstreams, backup/restore, ABP import, live telemetry, and a
// PAC endpoint. Static UI assets are served from embedded files.
//
// Routing handlers live in topic files alongside this one (config.go,
// upstreams.go, rules.go, data.go, stats.go, pac.go, pages.go). The
// reloader.go file owns the single path by which persisted changes get
// promoted to the running proxy.
package console

import (
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
)

//go:embed assets/*
var staticAssets embed.FS

type Server struct {
	db        *store.SQLite
	proxy     *proxy.Server
	telemetry *telemetry.Store
	sysStats  *telemetry.SysStatsCollector

	mu  sync.RWMutex
	cfg *config.Config

	mux http.Handler
}

func NewServer(cfg *config.Config, db *store.SQLite, proxyServer *proxy.Server, telemetryStore *telemetry.Store, sysStats *telemetry.SysStatsCollector) (*Server, error) {
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
		sysStats:  sysStats,
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
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/upstreams", s.handleUpstreams)
	mux.HandleFunc("/api/upstreams/", s.handleUpstreamByID)
	mux.HandleFunc("/api/rules", s.handleRules)
	mux.HandleFunc("/api/rules/", s.handleRuleByID)
	mux.HandleFunc("/api/rules/reorder", s.handleRuleReorder)
	mux.HandleFunc("/api/data/backup", s.handleDataBackup)
	mux.HandleFunc("/api/data/restore", s.handleDataRestore)
	mux.HandleFunc("/api/data/import-abp", s.handleDataImportABP)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/stats/history", s.handleStatsHistory)
	mux.HandleFunc("/api/activities", s.handleActivities)
	mux.HandleFunc("/api/activities/stream", s.handleActivitiesStream)
	mux.HandleFunc("/proxy.pac", s.handlePAC)

	s.mux = mux
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// --- shared helpers used by handler files ---

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
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

func parsePositiveIntDefault(raw string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func parseBoolForm(raw string, defaultValue bool) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return defaultValue
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func isForeignKeyConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "foreign key")
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
