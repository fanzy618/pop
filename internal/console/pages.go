package console

import (
	"net/http"

	"github.com/fanzy618/pop/internal/buildinfo"
)

var pagePaths = map[string]string{
	"/":           "assets/index.html",
	"/stats":      "assets/index.html",
	"/activities": "assets/activities.html",
	"/rules":      "assets/rules.html",
	"/upstreams":  "assets/upstreams.html",
	"/data":       "assets/data.html",
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	assetPath, ok := pagePaths[r.URL.Path]
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

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"version": buildinfo.Version})
}
