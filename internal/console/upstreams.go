package console

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/fanzy618/pop/internal/model"
)

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
		var up model.Upstream
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
		var up model.Upstream
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
