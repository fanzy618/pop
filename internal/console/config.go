package console

import (
	"encoding/json"
	"net/http"

	"github.com/fanzy618/pop/internal/config"
)

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
		rulesList, err := s.db.ListRules()
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
			"rules":          rulesList,
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

func (s *Server) updateConfig(next *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.applyBaseConfigLocked(next); err != nil {
		return err
	}
	return s.rebuildRuntimeLocked()
}

func (s *Server) applyBaseConfigLocked(next *config.Config) error {
	next = cloneConfig(next)
	if err := next.Validate(); err != nil {
		return err
	}
	s.cfg = next
	return nil
}
