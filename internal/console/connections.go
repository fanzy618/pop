package console

import (
	"net/http"

	"github.com/fanzy618/pop/internal/telemetry"
)

// handleConnections returns a snapshot of currently in-flight proxied
// requests. When the registry is not wired (e.g. tests with a bare proxy),
// the endpoint returns an empty array so callers can render without
// special-casing.
func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.connections == nil {
		writeJSON(w, http.StatusOK, []telemetry.ConnSnapshot{})
		return
	}
	writeJSON(w, http.StatusOK, s.connections.Snapshot())
}
