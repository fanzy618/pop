package console

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/fanzy618/pop/internal/abp"
	"github.com/fanzy618/pop/internal/model"
	"github.com/fanzy618/pop/internal/routing"
	"github.com/fanzy618/pop/internal/store"
)

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

func (s *Server) handleDataImportABP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	target, err := routing.ParseTarget(r.FormValue("route_target"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	enabled := parseBoolForm(r.FormValue("enabled"), true)

	raw, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "read file failed", http.StatusBadRequest)
		return
	}

	domains, totalLines, skippedUnsupported := abp.ParseDomains(string(raw))
	if len(domains) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                  true,
			"total_lines":         totalLines,
			"parsed_domains":      0,
			"created_rules":       0,
			"skipped_existing":    0,
			"skipped_unsupported": skippedUnsupported,
		})
		return
	}

	created := 0
	skippedExisting := 0
	batchSeen := make(map[string]struct{})
	for _, domain := range domains {
		normPattern := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "*."), ".")
		if _, ok := batchSeen[normPattern]; ok {
			skippedExisting++
			continue
		}
		batchSeen[normPattern] = struct{}{}
		rule := model.Rule{
			Enabled:    enabled,
			Pattern:    domain,
			Action:     target.Action,
			UpstreamID: target.UpstreamID,
		}
		if err := s.db.CreateRule(&rule); err != nil {
			if isForeignKeyConstraint(err) {
				http.Error(w, "unknown upstream_id", http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		created++
	}

	if err := s.rebuildRuntime(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"total_lines":         totalLines,
		"parsed_domains":      len(domains),
		"created_rules":       created,
		"skipped_existing":    skippedExisting,
		"skipped_unsupported": skippedUnsupported,
	})
}
