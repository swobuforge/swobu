package bootstrap

import (
	"net/http"
	"strings"
)

// readOnlyNodeControlGate rejects control-plane writes *before* handlers can
// materialize credentials, provision public ingress, or issue authorization.
// It does not block Workspace inference, MCP tool calls or runtime reservations.
// Data-plane endpoints are independently authorized by their normal policy.
func readOnlyNodeControlGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		mutating := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		// Stopping the daemon changes process lifetime, not desired Node state.
		// It must remain available when the configuration volume is read-only.
		if mutating && !(r.Method == http.MethodPost && path == "/api/v1/daemon/shutdown") &&
			(strings.HasPrefix(path, "/api/v1/") || path == "/api/v1") {
			http.Error(w, "config_read_only: Node desired state is externally managed", http.StatusConflict)
			return
		}
		next.ServeHTTP(w, r)
	})
}
