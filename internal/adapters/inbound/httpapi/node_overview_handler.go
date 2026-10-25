package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/swobuforge/swobu/internal/nodeauthority"
)

func NewNodeOverviewHandler(read func(context.Context) (nodeauthority.Overview, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !IsLocalOperatorRequest(r) {
			WriteControlProblem(w, r, *contractProblem(http.StatusForbidden, "NODE_AUTHORITY_REQUIRED", "node-authority-required", "Local Node administration is required."))
			return
		}
		if read == nil {
			WriteControlProblem(w, r, *contractProblem(http.StatusServiceUnavailable, "NODE_UNAVAILABLE", "node-unavailable", "Node configuration is unavailable."))
			return
		}
		result, err := read(r.Context())
		if err != nil || len(result.Revision) != 64 || (result.ConfigurationMode != "managed" && result.ConfigurationMode != "externally_managed") {
			WriteControlProblem(w, r, *contractProblem(http.StatusServiceUnavailable, "NODE_UNAVAILABLE", "node-unavailable", "Node configuration is unavailable."))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"`+result.Revision+`"`)
		_ = json.NewEncoder(w).Encode(result)
	})
}
