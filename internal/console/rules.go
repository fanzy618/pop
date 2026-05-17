package console

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/fanzy618/pop/internal/model"
	"github.com/fanzy618/pop/internal/store"
)

type rulesListResponse struct {
	Items    []model.Rule `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Keyword  string       `json:"keyword,omitempty"`
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page := parsePositiveIntDefault(r.URL.Query().Get("page"), 1)
		pageSize := parsePositiveIntDefault(r.URL.Query().Get("page_size"), 20)
		if pageSize > 100 {
			pageSize = 100
		}
		keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
		result, err := s.db.ListRulesPage(store.RuleListOptions{
			Keyword: keyword,
			Limit:   pageSize,
			Offset:  (page - 1) * pageSize,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		pageCount := max(1, (result.Total+pageSize-1)/pageSize)
		if page > pageCount {
			page = pageCount
			result, err = s.db.ListRulesPage(store.RuleListOptions{
				Keyword: keyword,
				Limit:   pageSize,
				Offset:  (page - 1) * pageSize,
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		writeJSON(w, http.StatusOK, rulesListResponse{
			Items:    result.Items,
			Total:    result.Total,
			Page:     page,
			PageSize: pageSize,
			Keyword:  keyword,
		})
	case http.MethodPost:
		var rule model.Rule
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
		var rule model.Rule
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

// Reorder is intentionally a no-op: ordering is implicit from created_at +
// pattern length. The endpoint exists for forward compatibility.
func (s *Server) handleRuleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reorder_disabled": true})
}
